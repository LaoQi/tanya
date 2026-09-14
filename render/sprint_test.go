package render

import (
	"testing"

	"github.com/LaoQi/tanyan/render/ir"
	"github.com/LaoQi/tanyan/render/term"
	"github.com/LaoQi/tanyan/render/theme"
)

func defSem() theme.Semantics {
	s, _ := theme.Lookup("default")
	return s.Sem
}

func TestSprintProfileNone(t *testing.T) {
	defer term.SetProfile(term.GetProfile())
	term.SetProfile(term.Profile{TTY: false, Colors: term.LevelNone})
	if got := defSem().Dim.Sprint("abc"); got != "abc" {
		t.Errorf("None profile 下应输出纯文本: %q", got)
	}
	if got := Sprint(ir.Span{Style: defSem().Info, Text: "a"}, ir.Span{Text: "b"}); got != "ab" {
		t.Errorf("None profile 混合: %q", got)
	}
}

func TestSprintMixedInlines(t *testing.T) {
	got := Sprint(ir.Span{Style: defSem().Dim, Text: "▸ "}, ir.Span{Style: defSem().Ok, Text: "ok"}, ir.Span{Text: " end"})
	want := "\x1b[90m▸ \x1b[0m\x1b[32mok\x1b[0m end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
