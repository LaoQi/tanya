package repl

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanya/render/term"

	"github.com/LaoQi/tanya/agent"
)

func TestHistoryLineStripsControlAndSGR(t *testing.T) {
	msgs := []agent.Message{
		{Role: "tool", Name: "run_shell", Content: "stdout:\nmock:200\n\r\x1b[K\x1b[33m⠋ 等待响应 0s\x1b[0m\r\x1b[Kpong\n\x1b[94m  ↳ 0ms\n\x1b[0m"},
		{Role: "tool", Name: "run_shell", Content: "stdout:\n--- A ---\n\r\x1b[K\x1b[33m" + strings.Repeat("长", 300)},
	}
	for i, m := range msgs {
		line := historyLine(i+1, m, 80)
		if strings.ContainsRune(line, 0x1b) || strings.ContainsRune(line, '\r') || strings.ContainsRune(line, 0x07) {
			t.Errorf("摘要行残留转义/控制字符: %q", line)
		}
		if w := term.Width(line); w > 79 {
			t.Errorf("摘要行未按终端列宽截断: %d", w)
		}
	}
}

func TestHistoryLineKeepsPlainText(t *testing.T) {
	m := agent.Message{Role: "assistant", Content: "第一行\n第二行"}
	if got, want := historyLine(2, m, 0), "  2 assistant 第一行 第二行"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestHistoryLineColumnWidthCJK 锁住截断宽度按显示列算：中文 100 字在 40 列终端上
// 必须截到一行内（按 rune 算会放过 200 列）。
func TestHistoryLineColumnWidthCJK(t *testing.T) {
	m := agent.Message{Role: "assistant", Content: strings.Repeat("字", 100)}
	line := historyLine(1, m, 40)
	if w := term.Width(line); w > 39 {
		t.Errorf("摘要行超宽 %d 列: %q", w, line)
	}
	if !strings.Contains(line, "~") {
		t.Errorf("截断应带标记: %q", line)
	}
}

// TestShowHistoryRespectsTerminalWidth 是接线用例：/history 的列宽必须取自 Console.Size()
// （fakeTerm 为 80×24），中文摘要按显示列截断到一行——而不是旧的 120 rune。
func TestShowHistoryRespectsTerminalWidth(t *testing.T) {
	dir := t.TempDir()
	seed := `{"role":"user","content":"` + strings.Repeat("长", 200) + `"}` + "\n"
	a := newSessTestAgent(t, dir)
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", seed)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	if err := a.LoadSession("20260101-100000"); err != nil {
		t.Fatal(err)
	}
	r.showHistory(nil)
	if !strings.Contains(out.String(), "~") {
		t.Fatalf("超长摘要应被截断，输出里应有截断标记: %q", out.String())
	}
	for _, l := range strings.Split(out.String(), "\n") {
		if w := term.Width(l); w > 79 {
			t.Errorf("/history 行超宽 %d 列（终端 80）: %q", w, l)
		}
	}
}

// TestTermColsUnavailable 锁住「宽度不可知 → 0」：没有 Console 时不猜宽度，调用方保持原样输出。
func TestTermColsUnavailable(t *testing.T) {
	r, _, _ := newTestREPLAgentProf(t, nil, nil, nonTTYProf)
	if got := r.termCols(); got != 0 {
		t.Errorf("无 Console 时 termCols 应为 0，实际 %d", got)
	}
}
