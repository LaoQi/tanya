package repl

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"
)

// renderToolEnd 还原一次完整工具块：ToolStart 的着色标题 + 追加式正文与状态行。
func renderToolEnd(sem theme.Semantics, name, args string, res agent.ToolResult, width, maxLines int) string {
	return sem.Dim.Frame(RenderToolStart(name, args, width)) + RenderToolEndAppend(sem, res, width, maxLines)
}

func TestRenderToolStart(t *testing.T) {
	got := RenderToolStart("run_shell", `{"command":"ls -la","timeout":60}`, 80)
	if !strings.Contains(got, "▸ run_shell") || !strings.Contains(got, "ls -la") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "⋯") {
		t.Errorf("追加语义下不应有进行中标记: %q", got)
	}
	if !strings.HasPrefix(got, "\n") || !strings.HasSuffix(got, "\n") {
		t.Errorf("应有前后空行包裹: %q", got)
	}
}

func TestRenderToolStartBadJSON(t *testing.T) {
	got := RenderToolStart("run_shell", `{bad`, 80)
	if !strings.Contains(got, `{bad`) {
		t.Errorf("解析失败应原样显示: %q", got)
	}
}

func TestRenderToolEndShortOutput(t *testing.T) {
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "line1\nline2\nline3\n"}},
		Duration: 250 * time.Millisecond,
		ExitCode: 0,
	}}
	got := renderToolEnd(testSem(), "run_shell", `{"command":"ls"}`, res, 80, 20)
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line3") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "↳ exit 0 · 250ms · 3 行") {
		t.Errorf("成功也应有完整状态行: %q", got)
	}
}

func TestRenderToolEndLongOutput(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 30; i++ {
		sb.WriteString("L" + strings.Repeat("x", i) + "\n")
	}
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout: []agent.ShellChunk{{Data: sb.String()}},
	}}
	got := renderToolEnd(testSem(), "run_shell", `{"command":"seq"}`, res, 80, 20)
	if strings.Contains(got, "L4x\n") || strings.Contains(got, "L28") {
		t.Errorf("中段行应被省略: %q", got)
	}
	if !strings.Contains(got, "Lx\n") || !strings.Contains(got, strings.Repeat("x", 29)) {
		t.Errorf("应保留首行 Lx 与末行 L+29x: %q", got)
	}
	if !strings.Contains(got, "共 30 行") {
		t.Errorf("应含总数提示: %q", got)
	}
}

func TestRenderToolEndFailStatus(t *testing.T) {
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stderr:   []agent.ShellChunk{{Data: "oops"}},
		ExitCode: 2,
	}}
	got := renderToolEnd(testSem(), "run_shell", `{"command":"false"}`, res, 80, 20)
	if !strings.Contains(got, "2| oops") {
		t.Errorf("stderr 应带 2| 标记: %q", got)
	}
	if !strings.Contains(got, "exit 2") {
		t.Errorf("应有退出码状态: %q", got)
	}
}

func TestRenderToolEndTimeoutInterrupt(t *testing.T) {
	res := agent.ToolResult{Meta: &agent.ShellResult{TimedOut: true, Duration: 3 * time.Second}}
	got := renderToolEnd(testSem(), "run_shell", `{"command":"sleep"}`, res, 80, 20)
	if !strings.Contains(got, "执行超时") || !strings.Contains(got, "3.0s") {
		t.Errorf("got %q", got)
	}
	res2 := agent.ToolResult{Meta: &agent.ShellResult{Interrupted: true}}
	if got := renderToolEnd(testSem(), "run_shell", `{}`, res2, 80, 20); !strings.Contains(got, "已中断") {
		t.Errorf("got %q", got)
	}
	res3 := agent.ToolResult{Meta: &agent.ShellResult{Interrupted: true, NotStarted: true}}
	if got := renderToolEnd(testSem(), "run_shell", `{}`, res3, 80, 20); !strings.Contains(got, "未执行") {
		t.Errorf("got %q", got)
	}
}

func TestRenderToolEndTruncateLongLine(t *testing.T) {
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout: []agent.ShellChunk{{Data: strings.Repeat("a", 200) + "\n"}},
	}}
	got := renderToolEnd(testSem(), "run_shell", `{"command":"cat"}`, res, 80, 20)
	if n := strings.Count(got, "\n"); n != 4 {
		t.Errorf("应为前导空行+标题+正文+状态行 4 行，实际 %d: %q", n, got)
	}
	if !strings.Contains(got, "~") {
		t.Errorf("超长行应以 ~ 结尾截断: %q", got)
	}
}

func TestRenderToolEndBuiltin(t *testing.T) {
	res := agent.ToolResult{Text: "1700000000 +0800 CST"}
	got := renderToolEnd(testSem(), "get_time", `{}`, res, 80, 20)
	if !strings.Contains(got, "▸ get_time") || !strings.Contains(got, "1700000000") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "{}") {
		t.Errorf("非 shell 工具不应显示空参数: %q", got)
	}
}

func TestRenderToolEndMetaFallback(t *testing.T) {
	cases := []struct {
		name string
		res  agent.ToolResult
	}{
		{"Meta 为 nil", agent.ToolResult{Text: "纯文本结果"}},
		{"Meta 异类型", agent.ToolResult{Text: "纯文本结果", Meta: "不是 ShellResult"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderToolEnd(testSem(), "run_shell", `{"command":"x"}`, c.res, 80, 20)
			if !strings.Contains(got, "纯文本结果") {
				t.Errorf("Meta 不可断言时应回落文本渲染: %q", got)
			}
		})
	}
}

func TestRenderToolEndBuiltinError(t *testing.T) {
	res := agent.ToolResult{Text: "error: 除数为零"}
	got := renderToolEnd(testSem(), "calc", `{"expression":"1/0"}`, res, 80, 20)
	if !strings.Contains(got, "error: 除数为零") {
		t.Errorf("got %q", got)
	}
}

func TestRenderToolEndTruncationMarker(t *testing.T) {
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout: []agent.ShellChunk{
			{Data: "head\n"},
			{Data: "tail\n", Truncated: 9999},
		},
	}}
	got := renderToolEnd(testSem(), "run_shell", `{}`, res, 80, 20)
	if !strings.Contains(got, "中间省略 9999 字节") {
		t.Errorf("应显示中间截断标记: %q", got)
	}
}

func TestTermFactsWidth(t *testing.T) {
	if w := (TermFacts{}).Width(); w != defaultToolWidth {
		t.Errorf("无尺寸应回落 %d: %d", defaultToolWidth, w)
	}
	if w := (TermFacts{Cols: 0, ColsOK: true}).Width(); w != defaultToolWidth {
		t.Errorf("零宽应回落 %d: %d", defaultToolWidth, w)
	}
	if w := (TermFacts{Cols: 120, ColsOK: true}).Width(); w != 120 {
		t.Errorf("注入宽度应生效: %d", w)
	}
}

func TestSemanticColors(t *testing.T) {
	if got := testSem().Dim.Sprint("abc"); got != "\x1b[90mabc\x1b[0m" {
		t.Errorf("Dim 应包暗灰: %q", got)
	}
	if got := testSem().Info.Sprint("abc"); got != "\x1b[94mabc\x1b[0m" {
		t.Errorf("Info 应包亮蓝: %q", got)
	}
	if got := testSem().Warn.Sprint("abc"); got != "\x1b[33mabc\x1b[0m" {
		t.Errorf("Warn 应包橙黄: %q", got)
	}
}

func TestRenderResponseInfoFull(t *testing.T) {
	info := agent.ResponseInfo{
		Duration:     3200 * time.Millisecond,
		FirstEvent:   800 * time.Millisecond,
		FirstContent: 3200 * time.Millisecond,
		Usage: &agent.Usage{
			PromptTokens:     12300,
			CompletionTokens: 1200,
			CacheHitTokens:   10045,
		},
		ContextTokens: 12300,
	}
	got := RenderResponseInfo(info, 80)
	for _, want := range []string{"↳", "TTFT 800ms", "TTFC 3.2s", "3.2s", "prompt 12.3k", "completion 1.2k", "缓存 81.67%"} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q: %q", want, got)
		}
	}
}

func TestRenderResponseInfoEstimate(t *testing.T) {
	info := agent.ResponseInfo{Duration: 1500 * time.Millisecond, ContextTokens: 800}
	got := RenderResponseInfo(info, 80)
	if !strings.Contains(got, "上下文 ~800") {
		t.Errorf("无 usage 应显示本地估算: %q", got)
	}
	if strings.Contains(got, "prompt") || strings.Contains(got, "缓存") {
		t.Errorf("无 usage 不应显示 prompt/缓存: %q", got)
	}
}

func TestRenderResponseInfoErrorPath(t *testing.T) {
	got := RenderResponseInfo(agent.ResponseInfo{Duration: 500 * time.Millisecond}, 80)
	if !strings.Contains(got, "↳ 500ms") {
		t.Errorf("出错路径应仅显示耗时: %q", got)
	}
	if got := RenderResponseInfo(agent.ResponseInfo{}, 80); got != "" {
		t.Errorf("全空 info 应无输出: %q", got)
	}
}

func TestRenderToolBlocksNoWrap(t *testing.T) {
	long := `{"command":"go build ./... && go vet ./... && go test ./repl/ ./style/ ./readline/ ./agent/ -count=1 2>&1 | tail -40"}`
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: strings.Repeat("输出内容宽字符测试", 30) + "\n"}},
		Duration: 250 * time.Millisecond,
		ExitCode: 0,
	}}
	for _, tc := range []struct {
		name string
		out  string
	}{
		{"start", RenderToolStart("run_shell", long, 80)},
		{"end", renderToolEnd(testSem(), "run_shell", long, res, 80, 20)},
	} {
		for _, l := range strings.Split(strings.TrimSuffix(tc.out, "\n"), "\n") {
			l = strings.ReplaceAll(l, "\r", "")
			if w := term.Width(l); w > 80 {
				t.Errorf("%s 行宽 %d 超出终端 80 列，折行会破坏块边界: %q", tc.name, w, l)
			}
		}
	}
}

func TestRenderResponseInfoNoTTFCWhenImmediate(t *testing.T) {
	info := agent.ResponseInfo{Duration: 800 * time.Millisecond, FirstEvent: 800 * time.Millisecond, FirstContent: 800 * time.Millisecond}
	got := RenderResponseInfo(info, 80)
	if !strings.Contains(got, "TTFT 800ms") {
		t.Errorf("应显示 TTFT: %q", got)
	}
	if strings.Contains(got, "TTFC") {
		t.Errorf("正文与首事件同时到达时不应显示 TTFC: %q", got)
	}
}

func TestToolViewStatusHeartbeat(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var buf syncBuf
	view := NewToolView(NewStreams(&buf, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	view.heart.interval = 2 * time.Millisecond
	view.heart.span = 3
	view.heart.now = stepClock(3 * time.Second)
	view.Handle(agent.Event{Kind: agent.EventRequestStart})
	if !strings.Contains(buf.String(), MsgStatusWaiting+" 0s") {
		t.Errorf("请求开始应打印带起始秒数的等待行: %q", buf.String())
	}
	waitUntil(t, "等待行换行", func() bool {
		return strings.Contains(term.Strip(buf.String()), MsgStatusWaiting+" 3s")
	})
	view.Handle(agent.Event{Kind: agent.EventReasoning, Text: "想"})
	waitUntil(t, "切到思考行", func() bool {
		return strings.Contains(term.Strip(buf.String()), MsgStatusThinking+" ")
	})
	view.Handle(agent.Event{Kind: agent.EventReasoning, Text: "想"})
	view.Handle(agent.Event{Kind: agent.EventResponse, Response: agent.ResponseInfo{Duration: time.Second}})
	out := buf.String()
	if !strings.Contains(term.Strip(out), MsgStatusWaiting) || !strings.Contains(term.Strip(out), MsgStatusThinking) {
		t.Errorf("等待行与思考行都应出现: %q", out)
	}
	if !strings.HasSuffix(term.Strip(out), "\n") {
		t.Errorf("停止心跳应收尾当前行: %q", out)
	}
	assertNoCursorControl(t, out)
}

func TestToolViewStopEndsHeartbeat(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var buf syncBuf
	view := NewToolView(NewStreams(&buf, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	view.heart.interval = 2 * time.Millisecond
	view.Handle(agent.Event{Kind: agent.EventRequestStart})
	waitUntil(t, "出现点", func() bool { return strings.Contains(term.Strip(buf.String()), ".") })
	view.Stop()
	first := buf.String()
	time.Sleep(30 * time.Millisecond)
	if got := buf.String(); got != first {
		t.Errorf("Stop 后不应再有心跳输出: %q -> %q", first, got)
	}
	view.Content(KindContent, "答案\n")
	if got := term.Strip(buf.String()); !strings.HasSuffix(strings.TrimSuffix(got, "答案\n"), "\n") {
		t.Errorf("状态行未收尾，正文粘连: %q", got)
	}
}

func TestToolViewInteractive(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var buf syncBuf
	view := NewToolView(NewStreams(&buf, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	view.heart.interval = 10 * time.Millisecond
	view.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"sudo -S true"}`, Interactive: true})
	time.Sleep(30 * time.Millisecond)
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"sudo -S true"}`, Interactive: true, Result: agent.ToolResult{Meta: &agent.ShellResult{Command: "sudo -S true", ExitCode: 1}}})
	out := buf.String()
	if !strings.Contains(out, "等待终端输入") {
		t.Errorf("交互模式应打印引导行: %q", out)
	}
	if out == "" {
		t.Fatal("输出为空则负向断言会静默通过")
	}
	if strings.Contains(out, "执行中") {
		t.Errorf("交互模式不应发心跳: %q", out)
	}
	if strings.Contains(out, "\x1b[1A") {
		t.Errorf("交互模式不应上移重绘: %q", out)
	}
	if n := strings.Count(out, "▸ run_shell"); n != 1 {
		t.Errorf("标题应只出现一次，实际 %d 次: %q", n, out)
	}
}

func TestToolViewNonInteractive(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var buf syncBuf
	view := NewToolView(NewStreams(&buf, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	view.heart.interval = 10 * time.Millisecond
	view.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`})
	time.Sleep(30 * time.Millisecond)
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`, Result: agent.ToolResult{Meta: &agent.ShellResult{Command: "echo hi", ExitCode: 0}}})
	out := buf.String()
	if out == "" {
		t.Fatal("输出为空则负向断言会静默通过")
	}
	if strings.Contains(out, "等待终端输入") {
		t.Errorf("非交互模式不应打印引导行: %q", out)
	}
	if !strings.Contains(out, MsgStatusRunning+" 0s") {
		t.Errorf("非交互模式应发心跳: %q", out)
	}
	if strings.Contains(out, "\x1b[1A") {
		t.Errorf("追加语义不应上移重绘: %q", out)
	}
	if n := strings.Count(out, "▸ run_shell"); n != 1 {
		t.Errorf("标题应只出现一次，实际 %d 次: %q", n, out)
	}
}

func TestRenderToolEndAppendNoTitle(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.Level16})
	t.Cleanup(func() { term.SetProfile(old) })
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "hi\n"}},
		Duration: 2 * time.Millisecond,
	}}
	got := RenderToolEndAppend(testSem(), res, 80, 20)
	if strings.Contains(got, "▸") {
		t.Errorf("追加式 ToolEnd 不应重复标题: %q", got)
	}
	plain := term.Strip(got)
	if !strings.HasPrefix(plain, "  hi\n") {
		t.Errorf("正文块应紧跟 ToolStart 输出（无前导空行）: %q", plain)
	}
	if !strings.Contains(plain, "↳ exit 0 · 2ms · 1 行") {
		t.Errorf("状态行应保留: %q", plain)
	}
	if got == "" {
		t.Fatal("输出为空则负向断言会静默通过")
	}
}

func TestRenderToolEndAppendKeepsCwdTitleOnlyOnce(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.Level16})
	t.Cleanup(func() { term.SetProfile(old) })
	args := `{"command":"ls -la","cwd":"/tmp/abc"}`
	res := agent.ToolResult{Text: "ok"}
	start := RenderToolStart("run_shell", args, 80)
	end := RenderToolEndAppend(testSem(), res, 80, 20)
	if n := strings.Count(start+end, "cwd: /tmp/abc"); n != 1 {
		t.Errorf("cwd 行应只在 ToolStart 出现一次，实际 %d 次: %q", n, start+end)
	}
	if strings.Contains(end, "▸") {
		t.Errorf("追加式 ToolEnd 不应含标题行: %q", end)
	}
}

func TestRenderToolEndANSIDirectView(t *testing.T) {
	oldProf := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.Level16})
	defer term.SetProfile(oldProf)
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "logo\n\x1b[90m版本行\x1b[0m\n"}},
		Duration: 100 * time.Millisecond,
	}}
	got := renderToolEnd(testSem(), "run_shell", `{"command":"printf"}`, res, 80, 20)
	if !strings.Contains(got, "  \x1b[90m版本行\x1b[0m\n") {
		t.Errorf("直显区应保留 SGR 原色: %q", got)
	}
	if !strings.Contains(got, "\x1b[90m\n▸ run_shell") {
		t.Errorf("标题行仍应 Dim 框定: %q", got)
	}
	pi := strings.Index(got, "版本行")
	si := strings.Index(got, "↳ exit 0")
	if pi < 0 || si < 0 || si < pi {
		t.Errorf("状态行应显式后置于直显区: %q", got)
	}
}

func TestRenderToolEndPlainBlockIntegrity(t *testing.T) {
	oldProf := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.Level16})
	defer term.SetProfile(oldProf)
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "a\x1b[2Kb\rc\n"}},
		Duration: 100 * time.Millisecond,
	}}
	got := renderToolEnd(testSem(), "run_shell", `{"command":"echo"}`, res, 80, 20)
	if strings.Contains(got, "\x1b[2K") || strings.Contains(got, "\r") {
		t.Errorf("布局序列与 C0 应被清洗: %q", got)
	}
	if strings.Count(got, "\x1b[90m") != 2 {
		t.Errorf("标题与正文各一次 Dim 开启: %q", got)
	}
	if strings.Count(got, "\x1b[0m") != 3 {
		t.Errorf("两处 Dim 与一处 Info 各一次闭合: %q", got)
	}
	if !strings.Contains(got, "\x1b[90m\n▸ run_shell") && !strings.Contains(got, "\x1b[90m▸ run_shell") {
		t.Errorf("块级包裹应覆盖标题与输出: %q", got)
	}
}

func TestRenderToolEndNonTTYNoEscape(t *testing.T) {
	oldProf := term.GetProfile()
	term.SetProfile(term.Profile{TTY: false, Colors: term.LevelNone})
	defer term.SetProfile(oldProf)
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "\x1b[31mred\x1b[0m\n"}},
		Duration: 100 * time.Millisecond,
	}}
	got := renderToolEnd(testSem(), "run_shell", `{"command":"echo"}`, res, 80, 20)
	if strings.Contains(got, "\x1b") {
		t.Errorf("非 TTY 输出不应含转义: %q", got)
	}
}

func TestRenderToolEndSGRMixedStderr(t *testing.T) {
	oldProf := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.Level16})
	defer term.SetProfile(oldProf)
	res := agent.ToolResult{Meta: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "plain\n"}},
		Stderr:   []agent.ShellChunk{{Data: "\x1b[91merr\x1b[0m\n"}},
		Duration: 100 * time.Millisecond,
	}}
	got := renderToolEnd(testSem(), "run_shell", `{}`, res, 80, 20)
	if !strings.Contains(got, "2| \x1b[91merr\x1b[0m\n") {
		t.Errorf("stderr 标记行彩色应直显保留: %q", got)
	}
}

// TestShellArgsViewKeepsMultiline 多行命令的折行只切不改：正文行去掉 `  $ ` 前缀后拼回原命令。
func TestShellArgsViewKeepsMultiline(t *testing.T) {
	args := `{"command":"cat > a <<'EOF'\n  line one \n\nline two\nEOF"}`
	v := shellArgsView(args, 80)
	if v.inline != "" {
		t.Errorf("多行命令不应内联: %q", v.inline)
	}
	var body []string
	for _, l := range v.body {
		body = append(body, strings.TrimPrefix(l, toolCommandPrefix))
	}
	if got, want := strings.Join(body, "\n"), "cat > a <<'EOF'\n  line one \n\nline two\nEOF"; got != want {
		t.Errorf("多行命令应保留换行与缩进（只去首尾空行）: got %q, want %q", got, want)
	}
}

func TestShellArgsViewCwd(t *testing.T) {
	cases := []struct {
		label, args string
		wantCwd     string
		wantCmd     string
	}{
		{"未指定 cwd", `{"command":"ls -la"}`, "", "ls -la"},
		{"显式 cwd 原样", `{"command":"ls","cwd":"/tmp/abc"}`, "/tmp/abc", "ls"},
		{"cwd 相对路径", `{"command":"ls","cwd":"sub"}`, "sub", "ls"},
		{"cwd 带空白", `{"command":"ls","cwd":" /tmp/a "}`, "/tmp/a", "ls"},
		{"cwd 空串", `{"command":"ls","cwd":""}`, "", "ls"},
	}
	for _, c := range cases {
		v := shellArgsView(c.args, 80)
		body := strings.Join(v.body, "\n")
		if c.wantCwd == "" {
			if strings.Contains(body, "cwd") || strings.Contains(body, "timeout") {
				t.Errorf("%s: 不应出现 cwd/timeout 行: %q", c.label, body)
			}
			if want := "\n▸ run_shell " + c.wantCmd + "\n"; v.inline != c.wantCmd {
				t.Errorf("%s: 应内联命令: got %q, want %q", c.label, v.inline, want)
			}
			continue
		}
		want := toolCwdPrefix + c.wantCwd + "\n" + toolCommandPrefix + c.wantCmd
		if body != want || v.inline != "" {
			t.Errorf("%s: got (%q, %q), want (%q, 内联为空)", c.label, body, v.inline, want)
		}
	}
	bad := shellArgsView(`{bad`, 80)
	if bad.inline != `{bad` || len(bad.body) != 1 || bad.body[0] != toolCommandPrefix+`{bad` {
		t.Errorf("坏 JSON 应原样展示: %+v", bad)
	}
	if empty := shellArgsView(`{}`, 80); empty.inline != "" || len(empty.body) != 0 {
		t.Errorf("空参数不应产生任何行: %+v", empty)
	}
}

// TestRenderToolStartTimeout 锁住 timeout 行：显式指定才显示，且与 cwd 一样把命令挤进块形态。
func TestRenderToolStartTimeout(t *testing.T) {
	cases := []struct {
		label string
		args  string
		want  string
	}{
		{"显式 timeout 转块", `{"command":"sleep 5","timeout":90}`, "\n▸ run_shell\n  timeout: 90s\n  $ sleep 5\n"},
		{"cwd 与 timeout 各一行", `{"command":"ls","cwd":"/tmp","timeout":30}`, "\n▸ run_shell\n  cwd: /tmp\n  timeout: 30s\n  $ ls\n"},
		{"零值 timeout 不显示", `{"command":"ls -la","timeout":0}`, "\n▸ run_shell ls -la\n"},
		{"默认省略保持内联", `{"command":"ls -la"}`, "\n▸ run_shell ls -la\n"},
	}
	for _, c := range cases {
		if got := RenderToolStart("run_shell", c.args, 80); got != c.want {
			t.Errorf("%s: got %q, want %q", c.label, got, c.want)
		}
	}
}

// TestRenderToolStartNonShellArgs 锁住通用键值参数区：内联 `key: value`、多参数 ` · ` 连接、块形态逐项一行。
func TestRenderToolStartNonShellArgs(t *testing.T) {
	cases := []struct {
		label, name, args, want string
	}{
		{"空参数只显示工具名", "get_time", `{}`, "\n▸ get_time\n"},
		{"单参数内联", "calc", `{"expression":"(1+2)*3/4"}`, "\n▸ calc expression: (1+2)*3/4\n"},
		{"多参数内联", "agent_custom", `{"action":"set","key":"model","value":"gpt-5"}`, "\n▸ agent_custom action: set · key: model · value: gpt-5\n"},
		{"数组值顿号连接", "get_env", `{"names":["HOME","PATH"]}`, "\n▸ get_env names: HOME, PATH\n"},
		{"空串值显式", "calc", `{"expression":""}`, "\n▸ calc expression: \"\"\n"},
		{"对象值紧凑 JSON", "calc", `{"expression":{"a":1}}`, "\n▸ calc expression: {\"a\":1}\n"},
		{"布尔与数字原样", "agent_custom", `{"action":"get","flag":true,"n":3}`, "\n▸ agent_custom action: get · flag: true · n: 3\n"},
	}
	for _, c := range cases {
		if got := RenderToolStart(c.name, c.args, 80); got != c.want {
			t.Errorf("%s: got %q, want %q", c.label, got, c.want)
		}
	}

	// 超宽内联候选转块：首行工具名、其后每个参数一行（值本身超出时再按宽度折行）。
	long := `{"action":"set","key":"model","value":"` + strings.Repeat("m", 80) + `"}`
	got := term.Strip(RenderToolStart("agent_custom", long, 80))
	lines := strings.Split(strings.Trim(got, "\n"), "\n")
	if len(lines) < 3 || lines[0] != "▸ agent_custom" || !strings.HasPrefix(lines[1], toolArgsPrefix+"action: set") {
		t.Fatalf("超宽参数应转块形态逐项一行: %q", lines)
	}
	joined := ""
	for _, l := range lines[1:] {
		joined += strings.TrimPrefix(l, toolArgsPrefix)
	}
	if want := "action: setkey: modelvalue: " + strings.Repeat("m", 80); joined != want {
		t.Errorf("折行只切不改: got %q, want %q", joined, want)
	}
	for _, l := range lines {
		if w := term.Width(l); w > 80 {
			t.Errorf("行宽 %d 越界 80: %q", w, l)
		}
	}
}

// TestRenderToolStartNonShellMultiline 多行值转块形态：逐行折行、只切不改，且每行不超终端宽度。
func TestRenderToolStartNonShellMultiline(t *testing.T) {
	got := term.Strip(RenderToolStart("agent_custom", `{"key":"a\nb"}`, 80))
	if want := "\n▸ agent_custom\n  key: a\n  b\n"; got != want {
		t.Errorf("多行值应转块形态: got %q, want %q", got, want)
	}
	long := `{"expression":"` + strings.Repeat("中", 200) + `"}`
	lines := strings.Split(strings.Trim(RenderToolStart("calc", long, 40), "\n"), "\n")
	if len(lines) < 3 || lines[0] != "▸ calc" {
		t.Fatalf("超长值应折行成块形态: %q", lines)
	}
	for _, l := range lines[1:] {
		if w := term.Width(l); w > 40 {
			t.Errorf("行宽 %d 越界 40: %q", w, l)
		}
		if !strings.HasPrefix(l, toolArgsPrefix) {
			t.Errorf("正文行应带 %q 前缀: %q", toolArgsPrefix, l)
		}
	}
}

// TestRenderToolStartNonShellBadJSON 坏 JSON 回退原样展示（不静默丢参数），且内联/块形态都不越界。
func TestRenderToolStartNonShellBadJSON(t *testing.T) {
	if got := RenderToolStart("calc", `{bad`, 80); got != "\n▸ calc {bad\n" {
		t.Errorf("坏 JSON 应原样内联: %q", got)
	}
	long := "{" + strings.Repeat("z", 120)
	lines := strings.Split(strings.Trim(RenderToolStart("calc", long, 40), "\n"), "\n")
	for _, l := range lines {
		if w := term.Width(l); w > 40 {
			t.Errorf("行宽 %d 越界 40: %q", w, l)
		}
	}
}

// TestRenderToolStartNonShellArgsOmitted 通用参数行数上限：保留头尾、中段换成参数专用省略文案。
func TestRenderToolStartNonShellArgsOmitted(t *testing.T) {
	// 反引号里的 \n 是字面两字符，恰为 JSON 转义换行：值解析后是 20 行，走 generic 键值渲染的省略路径。
	args := `{"key":"` + strings.Repeat(`x\n`, 20) + `"}`
	got := term.Strip(RenderToolStart("agent_custom", args, 80))
	body := strings.Split(strings.Trim(got, "\n"), "\n")[1:]
	if len(body) != toolCommandMaxLines {
		t.Fatalf("参数行数应为上限 %d，实际 %d: %q", toolCommandMaxLines, len(body), body)
	}
	want := toolArgsPrefix + fmt.Sprintf(MsgArgsOmittedFmt, 20-toolCommandHeadLines-toolCommandTailLines)
	if body[toolCommandHeadLines] != want {
		t.Errorf("省略行 = %q, want %q", body[toolCommandHeadLines], want)
	}
	if !strings.Contains(got, "完整参数见 /history") {
		t.Errorf("非 shell 工具应提示完整参数: %q", got)
	}

	// 窄终端下省略行同样按可用宽截断：加前缀后不越终端（回归：曾按终端总宽截断，越界 2 列）。
	narrow := strings.Split(strings.Trim(term.Strip(RenderToolStart("agent_custom", args, 26)), "\n"), "\n")
	if len(narrow) < 2 {
		t.Fatalf("窄终端应仍有标题与省略行: %q", narrow)
	}
	for _, l := range narrow {
		if w := term.Width(l); w > 26 {
			t.Errorf("窄终端行宽 %d 越界 26: %q", w, l)
		}
	}

	// 坏 JSON 走 plainArgsView 兜底：省略行同样不越界。
	bad := "{" + strings.Repeat("z\n", 20)
	for _, l := range strings.Split(strings.Trim(term.Strip(RenderToolStart("calc", bad, 26)), "\n"), "\n") {
		if w := term.Width(l); w > 26 {
			t.Errorf("坏 JSON 窄终端行宽 %d 越界 26: %q", w, l)
		}
	}
	if !strings.Contains(term.Strip(RenderToolStart("calc", bad, 80)), "完整参数见 /history") {
		t.Error("坏 JSON 的省略行应使用参数专用文案")
	}
}

// TestParseArgPairsKeepsOrder 键序按模型给的原顺序（map 会按字母序重排）。
func TestParseArgPairsKeepsOrder(t *testing.T) {
	pairs, ok := parseArgPairs(`{"zeta":1,"alpha":2,"mid":"x"}`)
	if !ok {
		t.Fatal("合法 JSON 应解析成功")
	}
	var keys []string
	for _, p := range pairs {
		keys = append(keys, p.key)
	}
	if strings.Join(keys, ",") != "zeta,alpha,mid" {
		t.Errorf("键序应保持原文顺序: %q", keys)
	}
	if pairs[2].value != "x" {
		t.Errorf("值应解出: %q", pairs[2].value)
	}
	if _, ok := parseArgPairs(`[1,2]`); ok {
		t.Error("顶层非对象应判失败")
	}
	if _, ok := parseArgPairs(`{bad`); ok {
		t.Error("坏 JSON 应判失败")
	}
	if _, ok := parseArgPairs(`{"a":1} 尾随内容`); ok {
		t.Error("收尾 } 后还有内容应判失败（与 Unmarshal 口径一致，避免显示正常而执行报错）")
	}
}

func TestRenderToolStartCwd(t *testing.T) {
	got := RenderToolStart("run_shell", `{"command":"ls -la","cwd":"/tmp/abc"}`, 80)
	if !strings.HasPrefix(got, "\n▸ run_shell\n  cwd: /tmp/abc\n  $ ls -la\n") {
		t.Errorf("应为首行工具名、cwd 与命令各占一行: %q", got)
	}
	end := RenderToolEndAppend(testSem(), agent.ToolResult{Text: "ok"}, 80, 20)
	if strings.Contains(end, "▸ run_shell") || strings.Contains(end, "cwd: /tmp/abc") {
		t.Errorf("追加式收尾不应重复标题与 cwd 行: %q", end)
	}
	if n := strings.Count(got+end, "cwd: /tmp/abc"); n != 1 {
		t.Errorf("cwd 行应只出现一次，实际 %d 次", n)
	}
	plain := RenderToolStart("run_shell", `{"command":"ls -la"}`, 80)
	if want := "\n▸ run_shell ls -la\n"; plain != want {
		t.Errorf("got %q, want %q", plain, want)
	}
	if strings.Contains(plain, "cwd") {
		t.Errorf("未指定 cwd 不应出现 cwd 行: %q", plain)
	}
}

func TestRenderToolStartCwdWidth(t *testing.T) {
	long := `{"command":"` + strings.Repeat("y", 300) + `","cwd":"` + strings.Repeat("/seg", 50) + `"}`
	lines := strings.Split(strings.Trim(RenderToolStart("run_shell", long, 80), "\n"), "\n")
	if len(lines) < 3 || lines[0] != "▸ run_shell" || !strings.HasPrefix(lines[1], "  cwd: ") {
		t.Fatalf("显式 cwd 应为「工具名 / cwd / 命令区」块形态: %q", lines)
	}
	for _, line := range lines {
		if w := term.Width(line); w > 80 {
			t.Errorf("块内行宽 %d 越界: %q", w, line)
		}
	}
}

// TestRenderToolStartInline 锁住内联形态：命令单行且与工具名同行放得下时保持旧版逐字节形态。
func TestRenderToolStartInline(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		{"短命令", "ls -la"},
		{"恰好占满剩余宽度", strings.Repeat("z", 80-3-len("run_shell")-1)},
	}
	for _, c := range cases {
		args, _ := json.Marshal(map[string]string{"command": c.cmd})
		got := RenderToolStart("run_shell", string(args), 80)
		if want := "\n▸ run_shell " + c.cmd + "\n"; got != want {
			t.Errorf("%s: got %q, want %q", c.name, got, want)
		}
	}
}

// TestRenderToolStartBlockShape 锁住块形态：命令放不下（超宽或原本多行）时转块——首行只有工具名，
// 命令逐行 `  $ ` 前缀、按显示宽度折行，折行只切不改内容，且每行不超终端宽度。
func TestRenderToolStartBlockShape(t *testing.T) {
	cmds := []string{
		strings.Repeat("x", 500),
		"echo a\necho b",
		strings.Repeat("中", 100),
		"cd /tmp && ls\n" + strings.Repeat("y", 300),
		strings.Repeat("z", 80-3-len("run_shell")) + "w",
	}
	for _, cmd := range cmds {
		args, _ := json.Marshal(map[string]string{"command": cmd})
		lines := strings.Split(strings.Trim(term.Strip(RenderToolStart("run_shell", string(args), 80)), "\n"), "\n")
		if lines[0] != "▸ run_shell" {
			t.Errorf("块形态首行应只有工具名: %q", lines[0])
		}
		var body []string
		for _, l := range lines[1:] {
			if !strings.HasPrefix(l, toolCommandPrefix) {
				t.Errorf("命令行应带 %q 前缀: %q", toolCommandPrefix, l)
				continue
			}
			if w := term.Width(l); w > 80 {
				t.Errorf("命令行宽度 %d 越界: %q", w, l)
			}
			body = append(body, strings.TrimPrefix(l, toolCommandPrefix))
		}
		if joined := strings.Join(body, ""); joined != strings.ReplaceAll(cmd, "\n", "") {
			t.Errorf("折行改写了命令内容:\n got %q\nwant %q", joined, strings.ReplaceAll(cmd, "\n", ""))
		}
	}
}

// TestRenderToolStartCommandOmitted 锁住命令行数上限：超上限保留头尾、中段换成省略提示。
func TestRenderToolStartCommandOmitted(t *testing.T) {
	var lines []string
	for i := 1; i <= 12; i++ {
		lines = append(lines, fmt.Sprintf("step-%02d", i))
	}
	args, _ := json.Marshal(map[string]string{"command": strings.Join(lines, "\n")})
	got := term.Strip(RenderToolStart("run_shell", string(args), 80))
	body := strings.Split(strings.Trim(got, "\n"), "\n")[1:]
	if len(body) != toolCommandMaxLines {
		t.Fatalf("命令行数应为上限 %d，实际 %d: %q", toolCommandMaxLines, len(body), body)
	}
	wantOmitted := fmt.Sprintf(MsgCmdOmittedFmt, 12-toolCommandHeadLines-toolCommandTailLines)
	if body[toolCommandHeadLines] != toolCommandPrefix+wantOmitted {
		t.Errorf("省略行 = %q, want %q", body[toolCommandHeadLines], toolCommandPrefix+wantOmitted)
	}
	if body[0] != toolCommandPrefix+"step-01" || body[len(body)-1] != toolCommandPrefix+"step-12" {
		t.Errorf("应保留头尾: %q", body)
	}
}

// TestRenderToolStartTabsExpanded 锁住制表符摊平：宽度表把 \t 当单列，不摊平则折行位置与显示不符。
func TestRenderToolStartTabsExpanded(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"command": "if x; then\n\techo tab\nfi"})
	got := term.Strip(RenderToolStart("run_shell", string(args), 80))
	if strings.ContainsRune(got, '\t') {
		t.Errorf("命令行不应含制表符: %q", got)
	}
	if want := "  $ " + strings.Repeat(" ", toolTabWidth) + "echo tab"; !strings.Contains(got, want+"\n") {
		t.Errorf("制表符应摊平成 %d 空格: %q", toolTabWidth, got)
	}
	// 摊平后宽度计算才准：tab 命令在窄终端里也必须每行不越界
	for _, line := range strings.Split(strings.Trim(term.Strip(RenderToolStart("run_shell", string(args), 24)), "\n"), "\n") {
		if w := term.Width(line); w > 24 {
			t.Errorf("窄终端行宽 %d 越界: %q", w, line)
		}
	}
}

func TestToolBlockTitleSingleSpaceAndWidth(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{"内联", "ls -la", "▸ run_shell ls -la"},
		{"超长转块", strings.Repeat("z", 300), "▸ run_shell"},
		{"多行转块", "cat <<'EOF'\nbody\nEOF", "▸ run_shell"},
	}
	for _, c := range cases {
		args, _ := json.Marshal(map[string]string{"command": c.cmd})
		got := strings.Trim(term.Strip(renderToolEnd(testSem(), "run_shell", string(args), agent.ToolResult{Text: "ok"}, 80, 20)), "\n")
		line, _, _ := strings.Cut(got, "\n")
		if line != c.want {
			t.Errorf("%s: 标题行 = %q, want %q", c.name, line, c.want)
		}
		for _, l := range strings.Split(got, "\n") {
			if w := term.Width(l); w > 80 {
				t.Errorf("%s: 行宽 %d 超过终端 80 列: %q", c.name, w, l)
			}
		}
	}
}

func TestToolBlockSingleWrite(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var wc writeCounter
	view := NewToolView(NewStreams(&wc, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	view.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`})
	before := wc.count()
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`,
		Result: agent.ToolResult{Meta: &agent.ShellResult{Command: "echo hi", Stdout: []agent.ShellChunk{{Data: "hi\n"}}, ExitCode: 0}}})
	writes := wc.writes()[before:]
	if len(writes) == 0 || writes[0] != "\n" {
		t.Fatalf("收尾应先补一个换行收尾心跳行，实际 %q", writes)
	}
	var blocks []string
	for _, w := range writes[1:] {
		if w != "\n" {
			blocks = append(blocks, w)
		}
	}
	if len(blocks) != 1 {
		t.Errorf("工具收尾正文块应一次写完，实际 %d 次: %q", len(blocks), writes)
	}
	if len(blocks) == 1 && (!strings.Contains(blocks[0], "↳ exit 0") || !strings.Contains(blocks[0], "hi")) {
		t.Errorf("块内容不完整: %q", blocks[0])
	}
	got := wc.String()
	if !strings.Contains(got, "▸ run_shell echo hi") || !strings.Contains(got, "↳ exit 0") || !strings.Contains(got, "hi") {
		t.Errorf("块内容不完整: %q", got)
	}
}

func TestToolViewStateFields(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var buf syncBuf
	st := NewStreams(&buf, &syncBuf{}, modeRich)
	view := NewToolView(st, term.GetProfile(), testSem(), func() int { return 80 }, 20)
	if view.st != st || view.maxLines != 20 || !view.prof.TTY || view.width() != 80 {
		t.Errorf("构造应把 writer/profile/宽度/行数写成字段: %+v", view)
	}
	view.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`})
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`,
		Result: agent.ToolResult{Meta: &agent.ShellResult{Command: "echo hi", Stdout: []agent.ShellChunk{{Data: "hi\n"}}, ExitCode: 0}}})
	if !view.justEnded || view.dirty {
		t.Errorf("工具块结束应置 justEnded 并清 dirty: justEnded=%v dirty=%v", view.justEnded, view.dirty)
	}
	if !strings.Contains(buf.String(), "▸ run_shell") {
		t.Errorf("TTY 下应输出工具块: %q", buf.String())
	}
	if strings.Contains(buf.String(), "\x1b[1A") {
		t.Errorf("追加语义不应上移重绘: %q", buf.String())
	}
	view.Content(KindContent, "答案")
	if view.justEnded || !view.dirty {
		t.Errorf("无换行结尾的正文应保持 dirty: justEnded=%v dirty=%v", view.justEnded, view.dirty)
	}
	view.Content(KindContent, "结束\n")
	if view.dirty {
		t.Error("以换行结尾的正文应清 dirty")
	}
	if !strings.Contains(buf.String(), "\n答案结束\n") {
		t.Errorf("justEnded 时应先补空行再写正文: %q", buf.String())
	}
}

func TestToolViewContentSemantics(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: false, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var buf syncBuf
	view := NewToolView(NewStreams(&buf, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	view.Content(KindContent, "")
	if buf.String() != "" {
		t.Errorf("空文本不应输出: %q", buf.String())
	}
	view.Content(KindNotice, "提示\n")
	if buf.String() != "提示\n" {
		t.Errorf("Content 应按给定 Kind 输出: %q", buf.String())
	}
	view.Content(KindContent, "续写")
	if buf.String() != "提示\n续写" {
		t.Errorf("无 justEnded 时不应补空行: %q", buf.String())
	}
}

func TestToolStatusLineSanitized(t *testing.T) {
	ttyProfile(t, plainProf)
	var out syncBuf
	st := NewStreams(&out, &syncBuf{}, modePlainVerbose)
	view := NewToolView(st, plainProf, testSem(), func() int { return 80 }, 20)
	res := agent.ToolResult{Meta: &agent.ShellResult{Cwd: "/tmp/\x1b[2Kx", Err: "cwd 不存在或不是目录: /tmp/\x1b[2Kx"}}
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{}`, Result: res})
	got := out.String()
	if strings.Contains(got, "\x1b[2K") {
		t.Errorf("状态行泄露捕获序列: %q", got)
	}
	if !strings.Contains(got, "↳ 错误: cwd 不存在或不是目录: /tmp/x") {
		t.Errorf("状态行缺错误文本: %q", got)
	}
}
