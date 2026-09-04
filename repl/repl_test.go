package repl

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/LaoQi/tanyan/agent"
)

func TestRenderPrompt(t *testing.T) {
	got := renderPrompt("{cwd} {model} {usage} →", "~/p/t", "m1", "123")
	if got != "~/p/t m1 123 →" {
		t.Errorf("got %q", got)
	}
}

func TestRenderPromptUnknownKept(t *testing.T) {
	got := renderPrompt("{cwd} {date}", "p", "m", "1")
	if !strings.Contains(got, "{date}") {
		t.Errorf("未知占位符应保留原样: %q", got)
	}
}

func TestNewREPLEmptyTplFallback(t *testing.T) {
	r, err := NewREPL(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.promptTpl != agent.DefaultPrompt {
		t.Errorf("空模板应回退默认值: %q", r.promptTpl)
	}
}

func TestHistoryLine(t *testing.T) {
	m := agent.Message{Role: "user", Content: "你好"}
	if got := historyLine(1, m); got != "  1 user      你好" {
		t.Errorf("got %q", got)
	}
	long := agent.Message{Role: "assistant", Content: strings.Repeat("字", 130)}
	got := historyLine(2, long)
	if !strings.HasSuffix(got, "...") || !strings.Contains(got, strings.Repeat("字", 120)) {
		t.Errorf("超长应截断至 120 rune: %q", got)
	}
	var tc agent.ToolCall
	tc.ID = "1"
	tc.Type = "function"
	tc.Function.Name = "run_shell"
	call := agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}}
	if got := historyLine(3, call); !strings.Contains(got, "[调用 run_shell]") {
		t.Errorf("tool_calls 应显示调用: %q", got)
	}
	tool := agent.Message{Role: "tool", Name: "run_shell", Content: "结果"}
	if got := historyLine(4, tool); !strings.HasPrefix(got, "  4 run_shell ") {
		t.Errorf("tool 消息应显示工具名: %q", got)
	}
}

func TestPrintHistoryFullToolCalls(t *testing.T) {
	var tc agent.ToolCall
	tc.ID = "1"
	tc.Type = "function"
	tc.Function.Name = "calc"
	tc.Function.Arguments = `{"expression":"1+1"}`
	m := agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}}
	out := captureStdout(func() { printHistoryFull(2, m) })
	if !strings.Contains(out, "#2 assistant") || !strings.Contains(out, "→ calc {\"expression\":\"1+1\"}") {
		t.Errorf("全量输出异常: %q", out)
	}
}

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}
