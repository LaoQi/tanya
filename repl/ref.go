package repl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaoQi/tanya/render/term"
)

type RefOptions struct {
	Workspace func() string
}

type Ref struct {
	Path string
	Name string
	Size int64
}

func ParseRefs(line string, opt RefOptions) []Ref {
	var out []Ref
	for _, tok := range refTokens(line, opt) {
		path := resolveRefPath(tok, opt)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, Ref{Path: path, Name: filepath.Base(path), Size: info.Size()})
	}
	return out
}

func refsText(refs []Ref) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, fmt.Sprintf(MsgRefItemFmt, term.OneLine(ref.Name), formatBytes(ref.Size)))
	}
	return fmt.Sprintf(MsgRefListFmt, strings.Join(parts, MsgRefSep))
}

func refTokens(line string, opt RefOptions) []string {
	var out []string
	for i := 0; i < len(line); {
		if line[i] != '@' || (i > 0 && !isRefSpace(line[i-1])) {
			i++
			continue
		}
		j := i + 1
		if j < len(line) && (line[j] == '"' || line[j] == '\'') {
			quote := line[j]
			k := j + 1
			for k < len(line) && line[k] != quote {
				k++
			}
			if k >= len(line) {
				i = j
				continue
			}
			out = append(out, line[j+1:k])
			i = k + 1
			continue
		}
		if j >= len(line) || isRefSpace(line[j]) {
			i = j
			continue
		}
		end := refWordEnd(line, j)
		if e, ok := refPathEnd(line, j, refTokenLimit(line, j), opt); ok {
			end = e
		}
		out = append(out, line[j:end])
		i = end
	}
	return out
}

func refWordEnd(line string, start int) int {
	end := start
	for end < len(line) && !isRefSpace(line[end]) {
		end++
	}
	return end
}

func refTokenLimit(line string, start int) int {
	for k := start + 1; k < len(line); k++ {
		if line[k] == '@' && isRefSpace(line[k-1]) {
			return k - 1
		}
	}
	return len(line)
}

func refPathEnd(line string, start, hi int, opt RefOptions) (int, bool) {
	ends := refWordEnds(line, start, hi)
	for i := len(ends) - 1; i >= 0; i-- {
		if refPathExists(line[start:ends[i]], opt) {
			return ends[i], true
		}
	}
	return 0, false
}

func refWordEnds(line string, start, hi int) []int {
	var ends []int
	for p := start; p < hi; {
		for p < hi && isRefSpace(line[p]) {
			p++
		}
		q := p
		for q < hi && !isRefSpace(line[q]) {
			q++
		}
		if q > p {
			ends = append(ends, q)
		}
		p = q
	}
	return ends
}

func refPathExists(p string, opt RefOptions) bool {
	info, err := os.Stat(resolveRefPath(p, opt))
	return err == nil && info.Mode().IsRegular()
}

func resolveRefPath(p string, opt RefOptions) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if p == "~" {
				return home
			}
			p = filepath.Join(home, p[2:])
		}
	}
	if !filepath.IsAbs(p) {
		base := ""
		if opt.Workspace != nil {
			base = opt.Workspace()
		}
		if base != "" {
			p = filepath.Join(base, p)
		}
	}
	return filepath.Clean(p)
}

func isRefSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
