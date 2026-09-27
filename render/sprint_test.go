package render

import (
	"testing"

	"github.com/LaoQi/tanya/render/ir"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"
)

func defSem() theme.Semantics {
	s, _ := theme.Lookup("default")
	return s.Sem
}

func TestSprintProfileNone(t *testing.T) {
	prof := term.Profile{TTY: false, Colors: term.LevelNone}
	if got := defSem().Dim.With(prof).Sprint("abc"); got != "abc" {
		t.Errorf("None profile 下应输出纯文本: %q", got)
	}
	if got := Sprint(prof, ir.Span{Style: defSem().Info, Text: "a"}, ir.Span{Text: "b"}); got != "ab" {
		t.Errorf("None profile 混合: %q", got)
	}
}

func TestSprintMixedInlines(t *testing.T) {
	got := Sprint(term.Profile{TTY: true, Colors: term.Level16}, ir.Span{Style: defSem().Dim, Text: "▸ "}, ir.Span{Style: defSem().Ok, Text: "ok"}, ir.Span{Text: " end"})
	want := "\x1b[90m▸ \x1b[0m\x1b[32mok\x1b[0m end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
