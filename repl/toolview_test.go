package repl

import (
	"encoding/json"
	"github.com/LaoQi/tanyan/render/term"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanyan/agent"
)

func TestRenderToolStart(t *testing.T) {
	got := RenderToolStart("run_shell", `{"command":"ls -la","timeout":60}`, 80)
	if !strings.Contains(got, "▸ run_shell") || !strings.Contains(got, "ls -la") || !strings.Contains(got, "⋯") {
		t.Errorf("got %q", got)
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
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "line1\nline2\nline3\n"}},
		Duration: 250 * time.Millisecond,
		ExitCode: 0,
	}}
	got := RenderToolEnd(testSem(), "run_shell", `{"command":"ls"}`, res, 80, 20)
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
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout: []agent.ShellChunk{{Data: sb.String()}},
	}}
	got := RenderToolEnd(testSem(), "run_shell", `{"command":"seq"}`, res, 80, 20)
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
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stderr:   []agent.ShellChunk{{Data: "oops"}},
		ExitCode: 2,
	}}
	got := RenderToolEnd(testSem(), "run_shell", `{"command":"false"}`, res, 80, 20)
	if !strings.Contains(got, "2| oops") {
		t.Errorf("stderr 应带 2| 标记: %q", got)
	}
	if !strings.Contains(got, "exit 2") {
		t.Errorf("应有退出码状态: %q", got)
	}
}

func TestRenderToolEndTimeoutInterrupt(t *testing.T) {
	res := agent.ToolResult{Shell: &agent.ShellResult{TimedOut: true, Duration: 3 * time.Second}}
	got := RenderToolEnd(testSem(), "run_shell", `{"command":"sleep"}`, res, 80, 20)
	if !strings.Contains(got, "执行超时") || !strings.Contains(got, "3.0s") {
		t.Errorf("got %q", got)
	}
	res2 := agent.ToolResult{Shell: &agent.ShellResult{Interrupted: true}}
	if got := RenderToolEnd(testSem(), "run_shell", `{}`, res2, 80, 20); !strings.Contains(got, "已中断") {
		t.Errorf("got %q", got)
	}
	res3 := agent.ToolResult{Shell: &agent.ShellResult{Interrupted: true, NotStarted: true}}
	if got := RenderToolEnd(testSem(), "run_shell", `{}`, res3, 80, 20); !strings.Contains(got, "未执行") {
		t.Errorf("got %q", got)
	}
}

func TestRenderToolEndTruncateLongLine(t *testing.T) {
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout: []agent.ShellChunk{{Data: strings.Repeat("a", 200) + "\n"}},
	}}
	got := RenderToolEnd(testSem(), "run_shell", `{"command":"cat"}`, res, 80, 20)
	if n := strings.Count(got, "\n"); n != 4 {
		t.Errorf("应为前导空行+标题+正文+状态行 4 行，实际 %d: %q", n, got)
	}
	if !strings.Contains(got, "~") {
		t.Errorf("超长行应以 ~ 结尾截断: %q", got)
	}
}

func TestRenderToolEndBuiltin(t *testing.T) {
	res := agent.ToolResult{Text: "1700000000 +0800 CST"}
	got := RenderToolEnd(testSem(), "get_time", `{}`, res, 80, 20)
	if !strings.Contains(got, "▸ get_time") || !strings.Contains(got, "1700000000") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "{}") {
		t.Errorf("非 shell 工具不应显示空参数: %q", got)
	}
}

func TestRenderToolEndBuiltinError(t *testing.T) {
	res := agent.ToolResult{Text: "error: 除数为零"}
	got := RenderToolEnd(testSem(), "calc", `{"expression":"1/0"}`, res, 80, 20)
	if !strings.Contains(got, "error: 除数为零") {
		t.Errorf("got %q", got)
	}
}

func TestRenderToolEndTruncationMarker(t *testing.T) {
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout: []agent.ShellChunk{
			{Data: "head\n"},
			{Data: "tail\n", Truncated: 9999},
		},
	}}
	got := RenderToolEnd(testSem(), "run_shell", `{}`, res, 80, 20)
	if !strings.Contains(got, "中间省略 9999 字节") {
		t.Errorf("应显示中间截断标记: %q", got)
	}
}

func TestToolWidthFallback(t *testing.T) {
	if w := ToolWidth(); w <= 0 {
		t.Errorf("宽度应回退 80: %d", w)
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

func TestRenderToolEndInline(t *testing.T) {
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "ok\n"}},
		Duration: 300 * time.Millisecond,
	}}
	got := RenderToolEndInline(testSem(), "run_shell", `{"command":"echo ok"}`, res, 80, 20)
	if !strings.HasPrefix(got, "\x1b[1A\r\x1b[K\x1b[90m▸ run_shell") {
		t.Errorf("应以上移重绘开头且标题行框定: %q", got)
	}
	if !strings.Contains(got, "\x1b[94m  ↳ exit 0 · 300ms · 1 行") || !strings.Contains(got, "  ok\n") {
		t.Errorf("状态行应亮蓝且含耗时行数: %q", got)
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
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: strings.Repeat("输出内容宽字符测试", 30) + "\n"}},
		Duration: 250 * time.Millisecond,
		ExitCode: 0,
	}}
	for _, tc := range []struct {
		name string
		out  string
	}{
		{"start", RenderToolStart("run_shell", long, 80)},
		{"end", RenderToolEnd(testSem(), "run_shell", long, res, 80, 20)},
		{"inline", RenderToolEndInline(testSem(), "run_shell", long, res, 80, 20)},
	} {
		for _, l := range strings.Split(strings.TrimSuffix(tc.out, "\n"), "\n") {
			l = strings.ReplaceAll(l, "\r", "")
			if w := term.Width(l); w > 80 {
				t.Errorf("%s 行宽 %d 超出终端 80 列，折行会导致 CursorUp 擦错行: %q", tc.name, w, l)
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

func TestToolViewReasoningLabel(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var buf syncBuf
	view := NewToolView(NewStreams(&buf, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	view.Handle(agent.Event{Kind: agent.EventRequestStart})
	time.Sleep(150 * time.Millisecond)
	view.Handle(agent.Event{Kind: agent.EventReasoning, Text: "想"})
	time.Sleep(150 * time.Millisecond)
	view.Handle(agent.Event{Kind: agent.EventResponse, Response: agent.ResponseInfo{Duration: time.Second}})
	out := buf.String()
	if !strings.Contains(out, "等待响应") {
		t.Errorf("请求开始应显示等待响应: %q", out)
	}
	if !strings.Contains(out, "思考中") {
		t.Errorf("收到思维链应切换为思考中: %q", out)
	}
}

func TestToolViewInteractive(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var buf syncBuf
	view := NewToolView(NewStreams(&buf, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	view.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"sudo -S true"}`, Interactive: true})
	time.Sleep(250 * time.Millisecond)
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"sudo -S true"}`, Interactive: true, Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "sudo -S true", ExitCode: 1}}})
	out := buf.String()
	if !strings.Contains(out, "等待终端输入") {
		t.Errorf("交互模式应打印引导行: %q", out)
	}
	if out == "" {
		t.Fatal("输出为空则负向断言会静默通过")
	}
	if strings.Contains(out, "执行中") {
		t.Errorf("交互模式不应启动 spinner: %q", out)
	}
	if strings.Contains(out, "\x1b[1A") {
		t.Errorf("交互模式不应上移重绘: %q", out)
	}
}

func TestToolViewNonInteractive(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var buf syncBuf
	view := NewToolView(NewStreams(&buf, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	view.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`})
	time.Sleep(250 * time.Millisecond)
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`, Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "echo hi", ExitCode: 0}}})
	out := buf.String()
	if out == "" {
		t.Fatal("输出为空则负向断言会静默通过")
	}
	if strings.Contains(out, "等待终端输入") {
		t.Errorf("非交互模式不应打印引导行: %q", out)
	}
	if !strings.Contains(out, "执行中") {
		t.Errorf("非交互模式应启动 spinner: %q", out)
	}
	if !strings.Contains(out, "\x1b[1A") {
		t.Errorf("非交互模式应上移重绘标题: %q", out)
	}
}

func TestRenderToolEndANSIDirectView(t *testing.T) {
	oldProf := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.Level16})
	defer term.SetProfile(oldProf)
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "logo\n\x1b[90m版本行\x1b[0m\n"}},
		Duration: 100 * time.Millisecond,
	}}
	got := RenderToolEnd(testSem(), "run_shell", `{"command":"printf"}`, res, 80, 20)
	if !strings.Contains(got, "  \x1b[90m版本行\x1b[0m\n") {
		t.Errorf("直显区应保留 SGR 原色: %q", got)
	}
	if !strings.Contains(got, "\x1b[90m▸ run_shell") {
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
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "a\x1b[2Kb\rc\n"}},
		Duration: 100 * time.Millisecond,
	}}
	got := RenderToolEnd(testSem(), "run_shell", `{"command":"echo"}`, res, 80, 20)
	if strings.Contains(got, "\x1b[2K") || strings.Contains(got, "\r") {
		t.Errorf("布局序列与 C0 应被清洗: %q", got)
	}
	if strings.Count(got, "\x1b[90m") != 1 {
		t.Errorf("单色块应单一 Dim 开启: %q", got)
	}
	if strings.Count(got, "\x1b[0m") != 2 {
		t.Errorf("Dim 与 Info 各一次闭合: %q", got)
	}
	if !strings.Contains(got, "\x1b[90m\n▸ run_shell") && !strings.Contains(got, "\x1b[90m▸ run_shell") {
		t.Errorf("块级包裹应覆盖标题与输出: %q", got)
	}
}

func TestRenderToolEndNonTTYNoEscape(t *testing.T) {
	oldProf := term.GetProfile()
	term.SetProfile(term.Profile{TTY: false, Colors: term.LevelNone})
	defer term.SetProfile(oldProf)
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "\x1b[31mred\x1b[0m\n"}},
		Duration: 100 * time.Millisecond,
	}}
	got := RenderToolEnd(testSem(), "run_shell", `{"command":"echo"}`, res, 80, 20)
	if strings.Contains(got, "\x1b") {
		t.Errorf("非 TTY 输出不应含转义: %q", got)
	}
}

func TestRenderToolEndSGRMixedStderr(t *testing.T) {
	oldProf := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.Level16})
	defer term.SetProfile(oldProf)
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "plain\n"}},
		Stderr:   []agent.ShellChunk{{Data: "\x1b[91merr\x1b[0m\n"}},
		Duration: 100 * time.Millisecond,
	}}
	got := RenderToolEnd(testSem(), "run_shell", `{}`, res, 80, 20)
	if !strings.Contains(got, "2| \x1b[91merr\x1b[0m\n") {
		t.Errorf("stderr 标记行彩色应直显保留: %q", got)
	}
}

func TestToolArgsDisplayCollapsesMultiline(t *testing.T) {
	cwd, cmd := toolArgsDisplay("run_shell", `{"command":"cat > a <<'EOF'\n  line one \n\nline two\nEOF"}`)
	if cwd != "" {
		t.Errorf("未指定 cwd 应为空: %q", cwd)
	}
	if want := "cat > a <<'EOF'; line one; line two; EOF"; cmd != want {
		t.Errorf("got %q, want %q", cmd, want)
	}
}

func TestToolArgsDisplayCwd(t *testing.T) {
	cases := []struct{ name, args, wantCwd, wantCmd string }{
		{"未指定 cwd", `{"command":"ls -la","timeout":60}`, "", "ls -la"},
		{"显式 cwd 原样", `{"command":"ls","cwd":"/tmp/abc"}`, "/tmp/abc", "ls"},
		{"cwd 相对路径", `{"command":"ls","cwd":"sub"}`, "sub", "ls"},
		{"cwd 带空白", `{"command":"ls","cwd":" /tmp/a "}`, "/tmp/a", "ls"},
		{"cwd 空串", `{"command":"ls","cwd":""}`, "", "ls"},
		{"坏 JSON", `{bad`, "", "{bad"},
	}
	for _, c := range cases {
		cwd, cmd := toolArgsDisplay("run_shell", c.args)
		if cwd != c.wantCwd || cmd != c.wantCmd {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, cwd, cmd, c.wantCwd, c.wantCmd)
		}
	}
	if cwd, cmd := toolArgsDisplay("get_time", `{"cwd":"/tmp","command":"x"}`); cwd != "" || cmd != "" {
		t.Errorf("非 run_shell 不应显示参数: (%q, %q)", cwd, cmd)
	}
}

func TestRenderToolStartCwd(t *testing.T) {
	got := RenderToolStart("run_shell", `{"command":"ls -la","cwd":"/tmp/abc"}`, 80)
	if !strings.HasPrefix(got, "\n▸ run_shell ⋯\n  cwd: /tmp/abc\n  ls -la\n") {
		t.Errorf("应为首行工具名、cwd 与命令各占一行: %q", got)
	}
	block := RenderToolEnd(testSem(), "run_shell", `{"command":"ls -la","cwd":"/tmp/abc"}`, agent.ToolResult{Text: "ok"}, 80, 20)
	if !strings.Contains(block, "▸ run_shell\n  cwd: /tmp/abc\n  ls -la\n") {
		t.Errorf("结束标题应与起始同构: %q", block)
	}
	inline := RenderToolEndInline(testSem(), "run_shell", `{"command":"ls -la","cwd":"/tmp/abc"}`, agent.ToolResult{Text: "ok"}, 80, 20)
	if want := strings.Repeat(term.CursorUp(1)+term.ClearLineHome(), 3); !strings.HasPrefix(inline, want) {
		t.Errorf("三行标题应上移三行重绘: %q", inline)
	}
	// 未指定 cwd 时与旧版逐字节一致
	plain := RenderToolStart("run_shell", `{"command":"ls -la"}`, 80)
	if want := "\n▸ run_shell ls -la ⋯\n"; plain != want {
		t.Errorf("got %q, want %q", plain, want)
	}
	if strings.Contains(plain, "cwd") {
		t.Errorf("未指定 cwd 不应出现 cwd 行: %q", plain)
	}
}

func TestRenderToolStartCwdWidth(t *testing.T) {
	long := `{"command":"` + strings.Repeat("y", 300) + `","cwd":"` + strings.Repeat("/seg", 50) + `"}`
	for _, line := range strings.Split(strings.Trim(RenderToolStart("run_shell", long, 80), "\n"), "\n") {
		if w := term.Width(line); w > 78 {
			t.Errorf("标题行宽度 %d 越界: %q", w, line)
		}
	}
}

func TestRenderToolStartAlwaysSingleLine(t *testing.T) {
	cmds := []string{
		"ls -la",
		strings.Repeat("x", 500),
		"echo a\necho b",
		strings.Repeat("中", 100),
		"cd /tmp && ls\n" + strings.Repeat("y", 300),
	}
	for _, cmd := range cmds {
		args, _ := json.Marshal(map[string]string{"command": cmd})
		got := strings.Trim(RenderToolStart("run_shell", string(args), 80), "\n")
		if strings.Contains(got, "\n") {
			t.Errorf("占位行应为单行: %q", got)
		}
		if w := term.Width(got); w > 79 {
			t.Errorf("占位行宽度 %d 超过 width-1: %q", w, got)
		}
	}
}

func TestToolBlockTitleSingleSpaceAndWidth(t *testing.T) {
	cases := []string{"ls -la", strings.Repeat("z", 300), "cat <<'EOF'\nbody\nEOF"}
	for _, cmd := range cases {
		args, _ := json.Marshal(map[string]string{"command": cmd})
		got := strings.Trim(RenderToolEnd(testSem(), "run_shell", string(args), agent.ToolResult{Text: "ok"}, 80, 20), "\n")
		line, _, _ := strings.Cut(got, "\n")
		if strings.Contains(line, "run_shell  ") {
			t.Errorf("结束块标题应为单空格: %q", line)
		}
		if !strings.HasPrefix(term.Strip(line), "▸ run_shell ") {
			t.Errorf("标题前缀异常: %q", line)
		}
		if w := term.Width(line); w > 79 {
			t.Errorf("标题行宽度 %d 超过 width-1: %q", w, line)
		}
	}
}

func TestToolBlockSingleWrite(t *testing.T) {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: true, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
	var wc writeCounter
	view := NewToolView(NewStreams(&wc, &syncBuf{}, modeRich), term.GetProfile(), testSem(), func() int { return 80 }, 20)
	before := wc.count()
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`,
		Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "echo hi", Stdout: []agent.ShellChunk{{Data: "hi\n"}}, ExitCode: 0}}})
	if got := wc.count() - before; got != 1 {
		t.Errorf("工具块应一次写完（标题+正文+状态行），实际 %d 次", got)
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
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`,
		Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "echo hi", Stdout: []agent.ShellChunk{{Data: "hi\n"}}, ExitCode: 0}}})
	if !view.justEnded || view.dirty {
		t.Errorf("工具块结束应置 justEnded 并清 dirty: justEnded=%v dirty=%v", view.justEnded, view.dirty)
	}
	if !strings.Contains(buf.String(), "▸ run_shell") {
		t.Errorf("TTY 下应上移重绘块: %q", buf.String())
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
