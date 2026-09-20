package repl

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanya/render/term"

	"github.com/LaoQi/tanya/agent"
)

func TestTurnRendersMarkdownTable(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	r, out, _ := newTestREPL(t, newFakeTerm())
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventContent, Text: "| a | b |\n|---|---|\n| 1 | 2 |\n"})
	turn.End(nil)
	plain := term.Strip(out.String())
	want := "┌───┬───┐\n│ a │ b │\n├───┼───┤\n│ 1 │ 2 │\n└───┴───┘\n"
	if !strings.Contains(plain, want) {
		t.Errorf("表格应带外框渲染:\n%s", plain)
	}
}

func TestTurnTableStreamsOnFirstBodyRow(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	r, out, _ := newTestREPL(t, newFakeTerm())
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventContent, Text: "| a | b |\n|---"})
	if got := term.Strip(out.String()); strings.Contains(got, "┌") {
		t.Errorf("表头与分隔行未确认前不应上屏: %q", got)
	}
	turn.Handle(agent.Event{Kind: agent.EventContent, Text: "|---|\n"})
	if got := term.Strip(out.String()); strings.Contains(got, "┌") {
		t.Errorf("列宽未定前不应上屏: %q", got)
	}
	turn.Handle(agent.Event{Kind: agent.EventContent, Text: "| 1 | 2 |\n"})
	if got := term.Strip(out.String()); !strings.Contains(got, "│ 1 │ 2 │") {
		t.Errorf("首数据行应带出表头: %q", got)
	}
	turn.End(nil)
	if got := term.Strip(out.String()); !strings.Contains(got, "│ 1 │ 2 │\n└───┴───┘\n") {
		t.Errorf("回合收尾应补下框: %q", got)
	}
}

func TestTurnTableNarrowTerminalCompact(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	r, out, _ := newTestREPL(t, newFakeTerm())
	r.view.width = func() int { return 8 }
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventContent, Text: "| a | b |\n|---|---|\n| 1 | 2 |\n"})
	plain := term.Strip(out.String())
	if !strings.Contains(plain, "┌─┬─┐\n│a│b│\n") {
		t.Errorf("窄终端应切紧边距:\n%s", plain)
	}
}

func TestTurnTablePlainBypass(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: false, Colors: term.LevelNone})
	r, out, _ := newTestREPLMode(t, newFakeTerm(), modeRich, term.Profile{TTY: false, Colors: term.LevelNone})
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventContent, Text: "| a | b |\n|---|---|\n| 1 | 2 |\n"})
	turn.End(nil)
	if got := term.Strip(out.String()); strings.Contains(got, "┌") {
		t.Errorf("非 TTY 不应渲染表格: %q", got)
	}
}
