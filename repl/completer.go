package repl

import (
	"fmt"
	"github.com/LaoQi/tanyan/render/theme"
	"strings"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
)

var slashCommands = []string{"/help", "/new", "/load", "/context", "/history", "/model", "/think", "/theme", "/exit", "/quit"}

var effortCandidates = func() []string {
	out := make([]string, 0, len(agent.EffortLevels)+1)
	out = append(out, agent.EffortLevels...)
	return append(out, "off")
}()

type completer struct {
	listSessions func() ([]agent.SessionInfo, error)
	listModels   func() ([]string, error)
	modelCache   []string
	modelLoaded  bool
}

func (c *completer) isCommandContext(line string) bool {
	return strings.HasPrefix(line, "/") && !strings.Contains(line, " ")
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

func (c *completer) isThinkContext(line string) bool {
	if !strings.HasPrefix(line, "/think ") {
		return false
	}
	prefix := strings.TrimPrefix(line, "/think ")
	return !strings.Contains(prefix, " ")
}

func (c *completer) models() []string {
	if c.modelLoaded {
		return c.modelCache
	}
	c.modelLoaded = true
	if c.listModels != nil {
		if ids, err := c.listModels(); err == nil {
			c.modelCache = ids
		}
	}
	return c.modelCache
}

func (c *completer) suggest(line string) string {
	switch {
	case c.isCommandContext(line):
		for _, cmd := range slashCommands {
			if strings.HasPrefix(cmd, line) && cmd != line {
				return cmd[len(line):]
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
	switch {
	case c.isCommandContext(line):
		var out []readline.Completion
		for _, cmd := range slashCommands {
			if strings.HasPrefix(cmd, line) {
				out = append(out, readline.Completion{Insert: cmd, Display: cmd})
			}
		}
		return out
	case c.isLoadContext(line):
		prefix := strings.TrimPrefix(line, "/load ")
		var out []readline.Completion
		for _, s := range c.sessions() {
			if strings.HasPrefix(s.ID, prefix) {
				out = append(out, readline.Completion{
					Insert:  "/load " + s.ID,
					Display: fmt.Sprintf(PickCompleteItem, s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, s.Summary),
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
