package repl

import (
	"github.com/LaoQi/tanya/render"
	"github.com/LaoQi/tanya/render/theme"
	"strings"
	"testing"

	"github.com/LaoQi/tanya/agent"
)

func promptRender(t *testing.T, tpl string, vars map[string]string) string {
	t.Helper()
	tpl2, err := render.ParseTemplate(tpl, testSem())
	if err != nil {
		t.Fatal(err)
	}
	return tpl2.Render(func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	})
}

func TestRenderPrompt(t *testing.T) {
	got := promptRender(t, "{cwd} {model} {effort} {usage} {cache} {cache_rate} {usage_summary} →", map[string]string{
		"cwd": "~/p/t", "model": "m1", "effort": "high", "usage": "123",
		"cache": "980", "cache_rate": "81.67%", "usage_summary": "12.3k 81.67%",
	})
	if got != "~/p/t m1 high 123 980 81.67% 12.3k 81.67% →" {
		t.Errorf("got %q", got)
	}
}

func TestRenderPromptCacheEmpty(t *testing.T) {
	got := promptRender(t, "{usage}|{cache}|{cache_rate}|{usage_summary}", map[string]string{
		"usage": "1", "cache": "", "cache_rate": "", "usage_summary": "",
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
	got := promptRender(t, "[white]{cwd}[/] [green]{usage_summary}[/]", map[string]string{"cwd": "/p", "usage_summary": "ok"})
	if got != "\x1b[37m/p\x1b[0m \x1b[32mok\x1b[0m" {
		t.Errorf("markup 模板上色: %q", got)
	}
}

func TestNewREPLEmptyTplFallback(t *testing.T) {
	r, err := NewREPL(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.promptTpl != theme.DefaultPrompt {
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
	r, buf, _ := newTestREPL(t, newFakeTerm())
	r.printHistoryFull(2, m)
	out := buf.String()
	if !strings.Contains(out, "\x1b[97;1m\x1b[33m#2 assistant\x1b[0m\x1b[0m") {
		t.Errorf("消息头应按一级标题渲染: %q", out)
	}
	if !strings.Contains(out, "\n[调用 calc]\n") || !strings.Contains(out, "\n→ calc {\"expression\":\"1+1\"}\n") {
		t.Errorf("正文摘要与工具调用行应原样无颜色: %q", out)
	}
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

func TestWelcomeText(t *testing.T) {
	oldV, oldB := Version, BuildTime
	t.Cleanup(func() { Version, BuildTime = oldV, oldB })
	Version = "1.2.3"
	BuildTime = ""
	out := welcomeText()
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "输入 /help 查看命令") {
			line = l
		}
	}
	if !strings.Contains(line, "tanyan 1.2.3") {
		t.Errorf("版本应与帮助提示同行: %q", out)
	}
	if strings.Contains(out, "构建于") {
		t.Errorf("构建时间为空时不应显示构建时间: %q", out)
	}
	if strings.Contains(out, "直接输入内容") {
		t.Errorf("welcome 不应含已删除的输入提示: %q", out)
	}
	BuildTime = "2026-09-11 12:00"
	if out = welcomeText(); !strings.Contains(out, "tanyan 1.2.3（构建于 2026-09-11 12:00）") {
		t.Errorf("welcome 应含版本与构建时间: %q", out)
	}
}

func TestMdDefaultOn(t *testing.T) {
	r, _, _ := newTestREPL(t, newFakeTerm())
	if !r.mdEnabled() {
		t.Error("TTY + rich 下 markdown 渲染应默认开启")
	}
}

func TestStreamContentBypassNonTTY(t *testing.T) {
	r, buf, _ := newTestREPLMode(t, newFakeTerm(), modeRich, nonTTYProf)
	turn := r.beginTurn(nil)
	turn.writeContent("直接输出")
	out := buf.String()
	if out != "直接输出" {
		t.Errorf("非 TTY 应直通输出: %q", out)
	}
}

func TestPrintHistoryFullRenderedMarkdown(t *testing.T) {
	r, buf, _ := newTestREPL(t, newFakeTerm())
	if !r.mdEnabled() {
		t.Fatal("默认应开启渲染")
	}
	m := agent.Message{Role: "assistant", Content: "# 标题\n\n- a\n- b\n\n正文 **粗** 结尾\n"}
	r.printHistoryFull(1, m)
	out := buf.String()
	for _, want := range []string{"\x1b[97;1m\x1b[33m#1 assistant\x1b[0m\x1b[0m", "\x1b[97;1m标题\x1b[0m", "• a", "• b", "\x1b[1m粗\x1b[0m"} {
		if !strings.Contains(out, want) {
			t.Errorf("assistant 正文应走 Markdown 渲染，缺 %q: %q", want, out)
		}
	}
	if out == "" {
		t.Fatal("输出为空则负向断言会静默通过")
	}
	if strings.Contains(out, "# 标题\n") || strings.Contains(out, "- a\n") {
		t.Errorf("渲染开启时不应输出原始 Markdown 文本: %q", out)
	}
}

func TestPrintHistoryFullBypassNonTTY(t *testing.T) {
	r, buf, _ := newTestREPLMode(t, newFakeTerm(), modeRich, nonTTYProf)
	if r.mdEnabled() {
		t.Fatal("非 TTY 应旁路")
	}
	m := agent.Message{Role: "assistant", Content: "# 标题\n\n- a\n"}
	r.printHistoryFull(1, m)
	out := buf.String()
	if !strings.HasPrefix(out, "#1 assistant\n") || !strings.Contains(out, "# 标题") || !strings.Contains(out, "- a") {
		t.Errorf("旁路时应原样输出（含消息头）: %q", out)
	}
	if out == "" {
		t.Fatal("输出为空则负向断言会静默通过")
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("旁路时不应出现颜色: %q", out)
	}
}

func TestPrintHistoryFullUserToolRaw(t *testing.T) {
	r, buf, _ := newTestREPL(t, newFakeTerm())
	user := agent.Message{Role: "user", Content: "请解释 **这个** 不算 md"}
	r.printHistoryFull(1, user)
	out := buf.String()
	if !strings.Contains(out, "\n请解释 **这个** 不算 md\n") {
		t.Errorf("user 正文应原样显示不渲染: %q", out)
	}
	tool := agent.Message{Role: "tool", Name: "run_shell", Content: "输出 `code` 原文"}
	buf.Reset()
	r.printHistoryFull(2, tool)
	out = buf.String()
	if !strings.Contains(out, "\n\x1b[90m输出 `code` 原文\x1b[0m\n") {
		t.Errorf("tool 正文应 Frame 灰色清洗显示: %q", out)
	}
}

func TestNoSaveWarnOnlyWhenEnabled(t *testing.T) {
	r, err := NewREPL(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.noSaveWarn(); got != "" {
		t.Errorf("agent 为 nil 时不应输出警告: %q", got)
	}
	r.agent = newSessTestAgent(t, t.TempDir())
	if got := r.noSaveWarn(); got != "" {
		t.Errorf("默认可写模式不应输出警告: %q", got)
	}
	r.agent = newSessTestAgent(t, t.TempDir(), agent.NoSave(true))
	got := r.noSaveWarn()
	if !strings.Contains(got, MsgNoSaveWarn) || !strings.HasSuffix(got, "\n") {
		t.Errorf("只读模式应输出一行警告: %q", got)
	}
}

func TestPrintHistoryHeadRoleColor(t *testing.T) {
	r, buf, _ := newTestREPL(t, newFakeTerm())
	cases := []struct {
		role string
		name string
		want string
	}{
		{"user", "", "\x1b[32m#1 user\x1b[0m"},
		{"assistant", "", "\x1b[33m#1 assistant\x1b[0m"},
		{"tool", "run_shell", "\x1b[33m#1 run_shell\x1b[0m"},
	}
	for _, tc := range cases {
		buf.Reset()
		r.printHistoryHead(1, agent.Message{Role: tc.role, Name: tc.name, Content: "x"})
		out := buf.String()
		if !strings.Contains(out, tc.want) {
			t.Errorf("role=%s 消息头应着色为 %q: %q", tc.role, tc.want, out)
		}
		if !strings.Contains(out, "\x1b[97;1m") {
			t.Errorf("role=%s 消息头应保留一级标题样式: %q", tc.role, out)
		}
	}
}

func TestPrintHistoryHeadPlainNonTTY(t *testing.T) {
	r, buf, _ := newTestREPLMode(t, newFakeTerm(), modeRich, nonTTYProf)
	r.printHistoryHead(3, agent.Message{Role: "user", Content: "x"})
	if out := buf.String(); out != "#3 user\n" {
		t.Errorf("非 TTY 时消息头应原样无色: %q", out)
	}
}
