package repl

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LaoQi/tanyan/agent"
)

func TestHistoryLineStripsControlAndSGR(t *testing.T) {
	msgs := []agent.Message{
		{Role: "tool", Name: "run_shell", Content: "stdout:\nmock:200\n\r\x1b[K\x1b[33m⠋ 等待响应 0s\x1b[0m\r\x1b[Kpong\n\x1b[94m  ↳ 0ms\n\x1b[0m"},
		{Role: "tool", Name: "run_shell", Content: "stdout:\n--- A ---\n\r\x1b[K\x1b[33m" + strings.Repeat("长", 300)},
	}
	for i, m := range msgs {
		line := historyLine(i+1, m)
		if strings.ContainsRune(line, 0x1b) || strings.ContainsRune(line, '\r') || strings.ContainsRune(line, 0x07) {
			t.Errorf("摘要行残留转义/控制字符: %q", line)
		}
		if n := utf8.RuneCountInString(line); n > 140 {
			t.Errorf("摘要行未按 120 runes 截断: %d", n)
		}
	}
}

func TestHistoryLineKeepsPlainText(t *testing.T) {
	m := agent.Message{Role: "assistant", Content: "第一行\n第二行"}
	if got, want := historyLine(2, m), "  2 assistant 第一行 第二行"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
