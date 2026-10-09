package repl

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/term"
)

const MaxImageURLLen = 8192

type AttachOptions struct {
	Workspace func() string
	MaxBytes  int
	MaxCount  int
	Resize    bool
	Detail    string
}

func ParseAttachments(line string, opt AttachOptions) ([]agent.ImageRef, error) {
	tokens := attachTokens(line, opt)
	if len(tokens) == 0 {
		return nil, nil
	}
	max := opt.MaxCount
	if max <= 0 {
		max = agent.DefaultImageMaxCount
	}
	var out []agent.ImageRef
	for _, tok := range tokens {
		img, err := attachOne(tok, opt)
		if err != nil {
			return nil, err
		}
		if img == nil {
			continue
		}
		if len(out) >= max {
			return nil, fmt.Errorf(MsgImageTooMany, max)
		}
		out = append(out, *img)
	}
	return out, nil
}

type attachSpan struct {
	text       string
	start, end int
}

func attachSpans(line string, opt AttachOptions) []attachSpan {
	var out []attachSpan
	for i := 0; i < len(line); {
		if line[i] != '@' || (i > 0 && !isAttachSpace(line[i-1])) {
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
			out = append(out, attachSpan{text: line[j+1 : k], start: i, end: k + 1})
			i = k + 1
			continue
		}
		if j >= len(line) || isAttachSpace(line[j]) {
			i = j
			continue
		}
		end := attachWordEnd(line, j)
		if !isAttachLiteral(line[j:]) {
			if e, ok := attachPathEnd(line, j, attachTokenLimit(line, j), opt); ok {
				end = e
			}
		}
		out = append(out, attachSpan{text: line[j:end], start: i, end: end})
		i = end
	}
	return out
}

func isAttachLiteral(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "data:")
}

func attachWordEnd(line string, start int) int {
	end := start
	for end < len(line) && !isAttachSpace(line[end]) {
		end++
	}
	return end
}

func attachTokenLimit(line string, start int) int {
	for k := start + 1; k < len(line); k++ {
		if line[k] == '@' && isAttachSpace(line[k-1]) {
			return k - 1
		}
	}
	return len(line)
}

func attachPathEnd(line string, start, hi int, opt AttachOptions) (int, bool) {
	ends := attachWordEnds(line, start, hi)
	for i := len(ends) - 1; i >= 0; i-- {
		if attachPathExists(line[start:ends[i]], opt) {
			return ends[i], true
		}
	}
	return 0, false
}

func attachWordEnds(line string, start, hi int) []int {
	var ends []int
	for p := start; p < hi; {
		for p < hi && isAttachSpace(line[p]) {
			p++
		}
		q := p
		for q < hi && !isAttachSpace(line[q]) {
			q++
		}
		if q > p {
			ends = append(ends, q)
		}
		p = q
	}
	return ends
}

func attachPathExists(p string, opt AttachOptions) bool {
	info, err := os.Stat(resolveAttachPath(p, opt))
	return err == nil && info.Mode().IsRegular()
}

func attachTokens(line string, opt AttachOptions) []string {
	spans := attachSpans(line, opt)
	if len(spans) == 0 {
		return nil
	}
	out := make([]string, 0, len(spans))
	for _, sp := range spans {
		out = append(out, sp.text)
	}
	return out
}

func stripAttachTokens(line string, opt AttachOptions) string {
	var b strings.Builder
	prev := 0
	for _, sp := range attachSpans(line, opt) {
		b.WriteString(line[prev:sp.start])
		prev = sp.end
	}
	b.WriteString(line[prev:])
	return b.String()
}

func PrepareContent(line string, opt AttachOptions) (agent.Content, error) {
	imgs, err := ParseAttachments(line, opt)
	if err != nil {
		return agent.Content{}, err
	}
	if len(imgs) > 0 && strings.TrimSpace(stripAttachTokens(line, opt)) == "" {
		return agent.Content{}, errors.New(MsgImageNoText)
	}
	return agent.Content{Text: line, Images: imgs}, nil
}

func imagesText(images []agent.ImageRef) string {
	if len(images) == 0 {
		return ""
	}
	parts := make([]string, 0, len(images))
	for _, img := range images {
		parts = append(parts, fmt.Sprintf(MsgImageItemFmt, term.OneLine(img.Name), formatBytes(int64(img.Bytes))))
	}
	return fmt.Sprintf(MsgImageListFmt, strings.Join(parts, MsgImageSep))
}

func isAttachSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func attachOne(tok string, opt AttachOptions) (*agent.ImageRef, error) {
	switch {
	case strings.HasPrefix(tok, "data:"):
		return attachDataURL(tok, opt)
	case strings.HasPrefix(tok, "http://"), strings.HasPrefix(tok, "https://"):
		if len(tok) > MaxImageURLLen {
			return nil, fmt.Errorf(MsgImageURLTooLong, MaxImageURLLen)
		}
		return &agent.ImageRef{URL: tok, Name: urlName(tok)}, nil
	}
	path := resolveAttachPath(tok, opt)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	max := opt.MaxBytes
	if max <= 0 {
		max = agent.DefaultImageMaxBytes
	}
	if info.Size() > int64(max) {
		return nil, fmt.Errorf(MsgImageTooLarge, filepath.Base(path), max)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	mime := agent.SniffImageMIME(data)
	if mime == "" {
		return nil, fmt.Errorf(MsgImageBadFormat, filepath.Base(path))
	}
	data = MaybeResizeImage(data, mime, opt.Detail, opt.Resize)
	w, h := agent.ImageDimensions(data, mime)
	return &agent.ImageRef{
		MIME:   mime,
		Data:   base64.StdEncoding.EncodeToString(data),
		Name:   filepath.Base(path),
		Bytes:  len(data),
		Width:  w,
		Height: h,
	}, nil
}

func attachDataURL(tok string, opt AttachOptions) (*agent.ImageRef, error) {
	comma := strings.IndexByte(tok, ',')
	if comma < 0 || !strings.HasSuffix(tok[:comma], ";base64") {
		return nil, fmt.Errorf(MsgImageBadFormat, "data URL")
	}
	meta := strings.TrimSuffix(tok[5:comma], ";base64")
	mime := normImageMIME(meta)
	if mime == "" {
		return nil, fmt.Errorf(MsgImageBadFormat, "data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(tok[comma+1:])
	if err != nil {
		return nil, fmt.Errorf(MsgImageBadFormat, "data URL")
	}
	max := opt.MaxBytes
	if max <= 0 {
		max = agent.DefaultImageMaxBytes
	}
	if len(raw) > max {
		return nil, fmt.Errorf(MsgImageTooLarge, "data URL", max)
	}
	if agent.SniffImageMIME(raw) == "" {
		return nil, fmt.Errorf(MsgImageBadFormat, "data URL")
	}
	payload := MaybeResizeImage(raw, mime, opt.Detail, opt.Resize)
	w, h := agent.ImageDimensions(payload, mime)
	return &agent.ImageRef{MIME: mime, Data: base64.StdEncoding.EncodeToString(payload), Name: "data-url", Bytes: len(payload), Width: w, Height: h}, nil
}

func resolveAttachPath(p string, opt AttachOptions) string {
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

func urlName(u string) string {
	s := u
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return "url"
	}
	return s
}
