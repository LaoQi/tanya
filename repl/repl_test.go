package repl

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/style"
)

func promptRender(t *testing.T, tpl string, vars map[string]string) string {
	t.Helper()
	tpl2, err := style.ParseTemplate(tpl)
	if err != nil {
		t.Fatal(err)
	}
	return tpl2.Render(func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	})
}

func TestRenderPrompt(t *testing.T) {
	got := promptRender(t, "{cwd} {model} {effort} {usage} {cache} {cache_rate} {stat} →", map[string]string{
		"cwd": "~/p/t", "model": "m1", "effort": "high", "usage": "123",
		"cache": "980", "cache_rate": "81.67%", "stat": "980/12.3k 81.67%",
	})
	if got != "~/p/t m1 high 123 980 81.67% 980/12.3k 81.67% →" {
		t.Errorf("got %q", got)
	}
}

func TestRenderPromptCacheEmpty(t *testing.T) {
	got := promptRender(t, "{usage}|{cache}|{cache_rate}|{stat}", map[string]string{
		"usage": "1", "cache": "", "cache_rate": "", "stat": "",
	})
	if got != "1|||" {
		t.Errorf("无缓存数据 {cache} 应渲染为空: %q", got)
	}
}

func TestRenderPromptUnknownKept(t *testing.T) {
	got := promptRender(t, "{cwd} {date}", map[string]string{"cwd": "p"})
	if !strings.Contains(got, "{date}") {
		t.Errorf("未知占位符应保留原样: %q", got)
	}
}

func TestRenderPromptEffortEmpty(t *testing.T) {
	got := promptRender(t, "{model}[{effort}]", map[string]string{"model": "m", "effort": ""})
	if got != "m[]" {
		t.Errorf("未设置思考等级时 {effort} 应渲染为空: %q", got)
	}
}

func TestRenderPromptMarkupColored(t *testing.T) {
	got := promptRender(t, "[white]{cwd}[/] [green]{stat}[/]", map[string]string{"cwd": "/p", "stat": "ok"})
	if got != "\x1b[37m/p\x1b[0m \x1b[32mok\x1b[0m" {
		t.Errorf("markup 模板上色: %q", got)
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

func TestExitMessageConsistency(t *testing.T) {
	if MsgBye != "再见" {
		t.Errorf("退出文案应统一: %q", MsgBye)
	}
	if !strings.HasSuffix(MsgNewSession, "\n") || !strings.HasSuffix(MsgLoadedSess, "\n") {
		t.Errorf("repl 消息常量应以换行结尾，配合 Printf 单点控制换行")
	}
	if strings.Contains(MsgBye, "%") {
		t.Errorf("非格式化常量不应含动词: %q", MsgBye)
	}
}

func TestMdToggle(t *testing.T) {
	r, err := NewREPL(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !r.mdLive {
		t.Error("md 默认应开启")
	}
	if r.handleCommand("/md") {
		t.Error("/md 不应退出")
	}
	if r.mdLive {
		t.Error("/md 后应关闭")
	}
	r.handleCommand("/md")
	if !r.mdLive {
		t.Error("再次 /md 应开启")
	}
}

func TestDeltaFnBypassWhenOff(t *testing.T) {
	r, err := NewREPL(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.deltaFn() == nil {
		t.Fatal("deltaFn 不应为 nil")
	}
	r.mdLive = false
	if r.deltaFn() == nil {
		t.Error("关闭时仍应返回直通函数")
	}
}
