package repl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
)

var slashCommands = []string{"/help", "/new", "/switch", "/load", "/archive", "/fork", "/next", "/stat", "/history", "/model", "/think", "/reasoning", "/theme", "/exit", "/quit"}

var onOffCandidates = []string{"on", "off"}

var effortCandidates = func() []string {
	out := make([]string, 0, len(agent.EffortLevels)+1)
	out = append(out, agent.EffortLevels...)
	return append(out, "off")
}()

type completer struct {
	listSessions func() ([]agent.SessionInfo, error)
	listModels   func() ([]string, error)
	workspaceDir func() string
	modelCache   []string
	modelLoaded  bool
}

func (c *completer) isCommandContext(line string) bool {
	return strings.HasPrefix(line, "/") && !strings.Contains(line, " ")
}

func (c *completer) isSwitchContext(line string) bool {
	if !strings.HasPrefix(line, "/switch ") {
		return false
	}
	prefix := strings.TrimPrefix(line, "/switch ")
	return !strings.Contains(prefix, " ")
}

func (c *completer) isLoadContext(line string) bool {
	if !strings.HasPrefix(line, "/load ") {
		return false
	}
	prefix := strings.TrimPrefix(line, "/load ")
	return !strings.Contains(prefix, " ")
}

func (c *completer) isModelContext(line string) bool {
	if !strings.HasPrefix(line, "/model ") {
		return false
	}
	prefix := strings.TrimPrefix(line, "/model ")
	return !strings.Contains(prefix, " ")
}

func (c *completer) isThemeContext(line string) bool {
	if !strings.HasPrefix(line, "/theme ") {
		return false
	}
	prefix := strings.TrimPrefix(line, "/theme ")
	return !strings.Contains(prefix, " ")
}

func (c *completer) isReasoningContext(line string) bool {
	if !strings.HasPrefix(line, "/reasoning ") {
		return false
	}
	prefix := strings.TrimPrefix(line, "/reasoning ")
	return !strings.Contains(prefix, " ")
}

func (c *completer) isThinkContext(line string) bool {
	if !strings.HasPrefix(line, "/think ") {
		return false
	}
	prefix := strings.TrimPrefix(line, "/think ")
	return !strings.Contains(prefix, " ")
}

// models 装载服务端模型名并清洗为单行：候选 Display、ghost 建议与 Enter 后的 Insert
// 共用此缓存，三处落屏（菜单行/行内 ghost/进缓冲区逐帧重绘）都不再出现原始转义序列。
func (c *completer) models() []string {
	if c.modelLoaded {
		return c.modelCache
	}
	c.modelLoaded = true
	if c.listModels != nil {
		if ids, err := c.listModels(); err == nil {
			for i, id := range ids {
				ids[i] = term.OneLine(id)
			}
			c.modelCache = ids
		}
	}
	return c.modelCache
}

func refQuery(line string) (prefix, head string, quote byte, ok bool) {
	if strings.HasPrefix(line, "/") {
		return "", "", 0, false
	}
	start, q := refTokenStart(line)
	if start < 0 {
		return "", "", 0, false
	}
	if q != 0 {
		return line[start+2:], line[:start+1], q, true
	}
	return line[start+1:], line[:start+1], 0, true
}

func refTokenStart(line string) (int, byte) {
	start, quote := -1, byte(0)
	for i := 0; i < len(line); {
		if line[i] != '@' || (i > 0 && !isRefSpace(line[i-1])) {
			i++
			continue
		}
		j := i + 1
		if j < len(line) && (line[j] == '"' || line[j] == '\'') {
			q := line[j]
			k := j + 1
			for k < len(line) && line[k] != q {
				k++
			}
			if k >= len(line) {
				return i, q
			}
			start, quote = -1, 0
			i = k + 1
			continue
		}
		start, quote = i, 0
		i = j
	}
	return start, quote
}

func (c *completer) refQueryCandidates(line string) ([]switchCandidate, string, byte, bool) {
	prefix, head, quote, ok := refQuery(line)
	if !ok {
		return nil, "", 0, false
	}
	return c.refCandidates(prefix), head, quote, true
}

func (c *completer) refCandidates(prefix string) []switchCandidate {
	base, typedRoot, namePart, ok := c.candidateBase(prefix)
	if !ok {
		return nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	dot := strings.HasPrefix(namePart, ".")
	var out []switchCandidate
	if namePart == ".." {
		out = append(out, switchCandidate{path: typedRoot + "../", label: "../"})
	}
	for _, e := range entries {
		name := e.Name()
		if !refSafeName(name) {
			continue
		}
		if strings.HasPrefix(name, ".") && !dot {
			continue
		}
		if !strings.HasPrefix(name, namePart) {
			continue
		}
		if switchDirEntry(base, e) {
			out = append(out, switchCandidate{path: typedRoot + name + "/", label: term.OneLine(name + "/")})
			continue
		}
		if !refDirEntry(base, e) {
			continue
		}
		out = append(out, switchCandidate{path: typedRoot + name, label: term.OneLine(name)})
	}
	return out
}

func refSafeName(name string) bool {
	if name == "" || name[0] == '"' || name[0] == '\'' {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f })
}

func refDirEntry(base string, e os.DirEntry) bool {
	if e.Type().IsRegular() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(base, e.Name()))
	return err == nil && info.Mode().IsRegular()
}

func (c *completer) suggest(line string) string {
	if cands, head, quote, ok := c.refQueryCandidates(line); ok && quote == 0 {
		for _, cand := range cands {
			full := head + cand.path
			if !strings.HasPrefix(full, line) {
				continue
			}
			if rest := full[len(line):]; rest != "" {
				return rest
			}
		}
	}
	switch {
	case c.isCommandContext(line):
		for _, cmd := range slashCommands {
			if strings.HasPrefix(cmd, line) && cmd != line {
				return cmd[len(line):]
			}
		}
	case c.isSwitchContext(line):
		prefix := strings.TrimPrefix(line, "/switch ")
		for _, cand := range c.switchCandidates(prefix) {
			if cand.path != prefix {
				return cand.path[len(prefix):]
			}
		}
	case c.isLoadContext(line):
		prefix := strings.TrimPrefix(line, "/load ")
		for _, s := range c.sessionIDs() {
			if strings.HasPrefix(s, prefix) && s != prefix {
				return s[len(prefix):]
			}
		}
	case c.isModelContext(line):
		prefix := strings.TrimPrefix(line, "/model ")
		for _, m := range c.models() {
			if strings.HasPrefix(m, prefix) && m != prefix {
				return m[len(prefix):]
			}
		}
	case c.isReasoningContext(line):
		prefix := strings.TrimPrefix(line, "/reasoning ")
		for _, v := range onOffCandidates {
			if strings.HasPrefix(v, prefix) && v != prefix {
				return v[len(prefix):]
			}
		}
	case c.isThinkContext(line):
		prefix := strings.TrimPrefix(line, "/think ")
		for _, e := range effortCandidates {
			if strings.HasPrefix(e, prefix) && e != prefix {
				return e[len(prefix):]
			}
		}
	case c.isThemeContext(line):
		prefix := strings.TrimPrefix(line, "/theme ")
		for _, n := range theme.Names() {
			if strings.HasPrefix(n, prefix) && n != prefix {
				return n[len(prefix):]
			}
		}
	}
	return ""
}

func (c *completer) complete(line string) []readline.Completion {
	if cands, head, _, ok := c.refQueryCandidates(line); ok {
		var out []readline.Completion
		for _, cand := range cands {
			out = append(out, readline.Completion{Insert: head + cand.path, Display: cand.label})
		}
		return out
	}
	switch {
	case c.isCommandContext(line):
		var out []readline.Completion
		for _, cmd := range slashCommands {
			if strings.HasPrefix(cmd, line) {
				out = append(out, readline.Completion{Insert: cmd, Display: cmd})
			}
		}
		return out
	case c.isSwitchContext(line):
		prefix := strings.TrimPrefix(line, "/switch ")
		var out []readline.Completion
		for _, cand := range c.switchCandidates(prefix) {
			out = append(out, readline.Completion{Insert: "/switch " + cand.path, Display: cand.label})
		}
		return out
	case c.isLoadContext(line):
		prefix := strings.TrimPrefix(line, "/load ")
		var out []readline.Completion
		for _, s := range c.sessions() {
			if strings.HasPrefix(s.ID, prefix) {
				out = append(out, readline.Completion{
					Insert:  "/load " + s.ID,
					Display: fmt.Sprintf(PickCompleteItem, s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, sessSummary(s)),
				})
			}
		}
		return out
	case c.isModelContext(line):
		prefix := strings.TrimPrefix(line, "/model ")
		var out []readline.Completion
		for _, m := range c.models() {
			if strings.HasPrefix(m, prefix) {
				out = append(out, readline.Completion{Insert: "/model " + m, Display: m})
			}
		}
		return out
	case c.isReasoningContext(line):
		prefix := strings.TrimPrefix(line, "/reasoning ")
		var out []readline.Completion
		for _, v := range onOffCandidates {
			if strings.HasPrefix(v, prefix) {
				out = append(out, readline.Completion{Insert: "/reasoning " + v, Display: v})
			}
		}
		return out
	case c.isThinkContext(line):
		prefix := strings.TrimPrefix(line, "/think ")
		var out []readline.Completion
		for _, e := range effortCandidates {
			if strings.HasPrefix(e, prefix) {
				out = append(out, readline.Completion{Insert: "/think " + e, Display: e})
			}
		}
		return out
	case c.isThemeContext(line):
		prefix := strings.TrimPrefix(line, "/theme ")
		var out []readline.Completion
		for _, n := range theme.Names() {
			if strings.HasPrefix(n, prefix) {
				out = append(out, readline.Completion{Insert: "/theme " + n, Display: n})
			}
		}
		return out
	}
	return nil
}

func (c *completer) sessions() []agent.SessionInfo {
	if c.listSessions == nil {
		return nil
	}
	list, err := c.listSessions()
	if err != nil {
		return nil
	}
	return list
}

func (c *completer) sessionIDs() []string {
	list := c.sessions()
	ids := make([]string, 0, len(list))
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	return ids
}

type switchCandidate struct {
	path  string
	label string
}

func (c *completer) candidateBase(prefix string) (base, typedRoot, namePart string, ok bool) {
	home, _ := os.UserHomeDir()
	switch {
	case prefix == "~":
		if home == "" {
			return "", "", "", false
		}
		base, typedRoot, namePart = home, "~/", ""
	case strings.HasPrefix(prefix, "~"):
		rest := prefix[1:]
		if home == "" || !strings.HasPrefix(rest, "/") {
			return "", "", "", false
		}
		dirPart, name := splitSwitchPath(rest)
		base, typedRoot, namePart = home+dirPart, "~"+dirPart, name
	case strings.HasPrefix(prefix, "/"):
		dirPart, name := splitSwitchPath(prefix)
		base, typedRoot, namePart = dirPart, dirPart, name
	default:
		if c.workspaceDir == nil || c.workspaceDir() == "" {
			return "", "", "", false
		}
		dirPart, name := splitSwitchPath(prefix)
		base, typedRoot, namePart = filepath.Join(c.workspaceDir(), dirPart), dirPart, name
	}
	return base, typedRoot, namePart, true
}

func (c *completer) switchCandidates(prefix string) []switchCandidate {
	base, typedRoot, namePart, ok := c.candidateBase(prefix)
	if !ok {
		return nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	dot := strings.HasPrefix(namePart, ".")
	var out []switchCandidate
	if namePart == ".." {
		out = append(out, switchCandidate{path: typedRoot + "../", label: "../"})
	}
	for _, e := range entries {
		name := e.Name()
		if !switchDirEntry(base, e) || !safeSwitchName(name) {
			continue
		}
		if strings.HasPrefix(name, ".") && !dot {
			continue
		}
		if !strings.HasPrefix(name, namePart) {
			continue
		}
		out = append(out, switchCandidate{path: typedRoot + name + "/", label: term.OneLine(name + "/")})
	}
	return out
}

func splitSwitchPath(p string) (dirPart, name string) {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i+1], p[i+1:]
	}
	return "", p
}

func switchDirEntry(base string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(base, e.Name()))
	return err == nil && info.IsDir()
}

func safeSwitchName(name string) bool {
	return name != "" && !strings.ContainsFunc(name, func(r rune) bool { return r <= ' ' || r == 0x7f })
}
