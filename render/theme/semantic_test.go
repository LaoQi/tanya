package theme

import (
	"testing"

	rstyle "github.com/LaoQi/tanyan/render/style"
	"github.com/LaoQi/tanyan/render/term"
)

func TestSemanticSGR(t *testing.T) {
	sem := baseSem(t)
	cases := []struct {
		name  string
		style rstyle.Style
		want  string
	}{
		{"dim", sem.Dim, "\x1b[90m"},
		{"info", sem.Info, "\x1b[94m"},
		{"warn", sem.Warn, "\x1b[33m"},
		{"ok", sem.Ok, "\x1b[32m"},
		{"error", sem.Error, "\x1b[91m"},
		{"accent", sem.Accent, "\x1b[7m"},
	}
	for _, c := range cases {
		if got := c.style.SGR(term.GetProfile()); got != c.want {
			t.Errorf("%s sgr = %q, want %q", c.name, got, c.want)
		}
	}
}
