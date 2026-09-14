package repl

import (
	"errors"
	"github.com/LaoQi/tanyan/render/term"
	"strings"
	"testing"

	"github.com/LaoQi/tanyan/agent"
)

var plainProf = term.Profile{TTY: true, Colors: term.LevelNone}

func TestParseMode(t *testing.T) {
	if _, err := ParseMode(false, true); err == nil {
		t.Error("--verbose 无 --plain 应报错")
	}
	cases := []struct {
		plain, verbose bool
		want           outMode
	}{
		{false, false, modeRich},
		{true, false, modePlain},
		{true, true, modePlainVerbose},
	}
	for _, c := range cases {
		got, err := ParseMode(c.plain, c.verbose)
		if err != nil || got != c.want {
			t.Errorf("ParseMode(%v,%v) = %v,%v want %v", c.plain, c.verbose, got, err, c.want)
		}
	}
}

func TestVisSetMatrix(t *testing.T) {
	cases := []struct {
		mode outMode
		kind Kind
		want bool
	}{
		{modeRich, KindContent, true},
		{modeRich, KindReasoning, true},
		{modeRich, KindToolBlock, true},
		{modeRich, KindToolStatus, true},
		{modeRich, KindNotice, true},
		{modeRich, KindDecor, true},
		{modeRich, KindError, true},
		{modeRich, KindSpinner, true},

		{modePlain, KindContent, true},
		{modePlain, KindNotice, true},
		{modePlain, KindReasoning, false},
		{modePlain, KindToolBlock, false},
		{modePlain, KindToolStatus, false},
		{modePlain, KindDecor, false},
		{modePlain, KindError, false},
		{modePlain, KindSpinner, false},

		{modePlainVerbose, KindContent, true},
		{modePlainVerbose, KindNotice, true},
		{modePlainVerbose, KindToolBlock, true},
		{modePlainVerbose, KindToolStatus, true},
		{modePlainVerbose, KindReasoning, false},
		{modePlainVerbose, KindDecor, false},
		{modePlainVerbose, KindError, false},
		{modePlainVerbose, KindSpinner, false},
	}
	for _, c := range cases {
		st := NewStreams(&syncBuf{}, &syncBuf{}, c.mode)
		if got := st.out.allows(c.kind); got != c.want {
			t.Errorf("mode=%d kind=%d 可见性 = %v want %v", c.mode, c.kind, got, c.want)
		}
		if !st.err.allows(c.kind) {
			t.Errorf("stderr 不应参与屏蔽（mode=%d kind=%d）", c.mode, c.kind)
		}
	}
}

func feedAskPath(st *streams, prof term.Profile) *toolView {
	view := NewToolView(st, prof, testSem(), func() int { return 80 }, 20)
	view.Handle(agent.Event{Kind: agent.EventRequestStart})
	view.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`})
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`,
		Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "echo hi", Stdout: []agent.ShellChunk{{Data: "hi\n"}}, ExitCode: 0}}})
	view.Handle(agent.Event{Kind: agent.EventResponse, Response: agent.ResponseInfo{FirstEvent: 300e6, Duration: 1200e6}})
	view.Handle(agent.Event{Kind: agent.EventContent, Text: "答案"})
	return view
}

func TestPlainAskStdoutIsAnswerOnly(t *testing.T) {
	ttyProfile(t, plainProf)
	var out, errb syncBuf
	st := NewStreams(&out, &errb, modePlain)
	feedAskPath(st, plainProf)
	st.End()
	got := out.String()
	if got != "答案\n" {
		t.Errorf("plain 的 stdout 应严格等于答案加一个结尾换行: %q", got)
	}
	if strings.ContainsAny(got, "\x1b\r") {
		t.Errorf("plain 不应含转义或回车: %q", got)
	}
	if strings.Contains(got, "▸") || strings.Contains(got, "↳") {
		t.Errorf("plain 不应含工具块与状态行装饰: %q", got)
	}
	if errb.String() != "" {
		t.Errorf("正常路径不应写 stderr: %q", errb.String())
	}
}

func TestPlainAskErrorsToStderr(t *testing.T) {
	ttyProfile(t, plainProf)
	var out, errb syncBuf
	st := NewStreams(&out, &errb, modePlain)
	feedAskPath(st, plainProf)
	st.Fail("\n"+MsgErrLineFmt+"\n", errors.New("boom"))
	st.End()
	if got := out.String(); got != "答案\n" {
		t.Errorf("错误不应污染 stdout: %q", got)
	}
	if !strings.Contains(errb.String(), "错误: boom") {
		t.Errorf("错误应写 stderr: %q", errb.String())
	}
}

func TestPlainVerboseKeepsToolText(t *testing.T) {
	ttyProfile(t, plainProf)
	var out, errb syncBuf
	st := NewStreams(&out, &errb, modePlainVerbose)
	feedAskPath(st, plainProf)
	st.End()
	got := out.String()
	for _, want := range []string{"▸ run_shell echo hi", "hi", "↳ exit 0", "↳ TTFT 300ms", "答案"} {
		if !strings.Contains(got, want) {
			t.Errorf("plain+verbose 应保留纯文本工具痕迹，缺 %q: %q", want, got)
		}
	}
	if strings.ContainsAny(got, "\x1b\r") {
		t.Errorf("plain+verbose 不应含转义或回车: %q", got)
	}
	if strings.Contains(got, "\x1b[1A") {
		t.Errorf("plain+verbose 不应上移重绘: %q", got)
	}
}

func TestPlainNoSpinnerEvenOnTTY(t *testing.T) {
	r, out, _ := newTestREPLMode(t, newFakeTerm(), modePlain, plainProf)
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventRequestStart})
	turn.Handle(agent.Event{Kind: agent.EventReasoning})
	if got := out.String(); got != "" {
		t.Errorf("plain 下 TTY 也不应输出动画或空行: %q", got)
	}
}

func TestPlainMasksDecorAndTools(t *testing.T) {
	r, out, _ := newTestREPLMode(t, newFakeTerm(), modePlain, plainProf)
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`})
	turn.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`,
		Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "echo hi", ExitCode: 0}}})
	turn.Handle(agent.Event{Kind: agent.EventResponse, Response: agent.ResponseInfo{Duration: 1e9}})
	turn.Handle(agent.Event{Kind: agent.EventContent, Text: "正文"})
	turn.End(nil)
	got := out.String()
	if strings.HasPrefix(got, "\n") {
		t.Errorf("plain 不应补首行空行: %q", got)
	}
	if strings.Contains(got, "▸") || strings.Contains(got, "↳") || strings.Contains(got, "─") {
		t.Errorf("plain 应屏蔽工具块、状态行与分隔线: %q", got)
	}
	if !strings.Contains(got, "正文") {
		t.Errorf("plain 应保留正文: %q", got)
	}
}

func TestPlainInteractiveKeepsNotice(t *testing.T) {
	r, out, errb := newTestREPLMode(t, newFakeTerm(), modePlain, plainProf)
	r.handleCommand("/help")
	if !strings.Contains(out.String(), "斜杠命令") {
		t.Errorf("plain 交互下命令反馈应保留（否则 /help 失联）: %q", out.String())
	}
	r.handleCommand("/think bogus")
	if !strings.Contains(errb.String(), "无效思考等级") {
		t.Errorf("plain 下错误仍应写 stderr: %q", errb.String())
	}
}

func TestEndTightensNewlineOnlyInPlain(t *testing.T) {
	var richOut, plainOut syncBuf
	rich := NewStreams(&richOut, &syncBuf{}, modeRich)
	rich.Content("答案\n")
	rich.End()
	if got := richOut.String(); got != "答案\n\n" {
		t.Errorf("rich 应沿用无条件补换行: %q", got)
	}
	plain := NewStreams(&plainOut, &syncBuf{}, modePlain)
	plain.Content("答案\n")
	plain.End()
	if got := plainOut.String(); got != "答案\n" {
		t.Errorf("plain 应只在缺少行尾换行时补: %q", got)
	}
	plain2 := NewStreams(&syncBuf{}, &syncBuf{}, modePlain)
	var b syncBuf
	plain2.out.setWriter(&b)
	plain2.Content("答案")
	plain2.End()
	if got := b.String(); got != "答案\n" {
		t.Errorf("plain 缺换行时应补一个: %q", got)
	}
}

func TestRichStreamsByteIdentical(t *testing.T) {
	nonTTY := term.Profile{TTY: false, Colors: term.LevelNone}
	ttyProfile(t, nonTTY)
	var out syncBuf
	st := NewStreams(&out, &syncBuf{}, modeRich)
	view := NewToolView(st, nonTTY, testSem(), func() int { return 80 }, 20)
	view.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`})
	view.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`,
		Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "echo hi", Stdout: []agent.ShellChunk{{Data: "hi\n"}}, ExitCode: 0}}})
	view.Handle(agent.Event{Kind: agent.EventResponse, Response: agent.ResponseInfo{FirstEvent: 300e6, Duration: 1200e6}})
	view.Handle(agent.Event{Kind: agent.EventContent, Text: "答案\n"})
	want := "\n▸ run_shell echo hi ⋯\n\n▸ run_shell echo hi\n  hi\n  ↳ exit 0 · 0ms · 1 行\n  ↳ TTFT 300ms · 1.2s\n\n答案\n"
	if got := out.String(); got != want {
		t.Errorf("rich 非 TTY 输出应为基线字节\n got %q\nwant %q", got, want)
	}

	tty := term.Profile{TTY: true, Colors: term.LevelNone}
	ttyProfile(t, tty)
	var out2 syncBuf
	st2 := NewStreams(&out2, &syncBuf{}, modeRich)
	view2 := NewToolView(st2, tty, testSem(), func() int { return 80 }, 20)
	view2.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`,
		Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "echo hi", Stdout: []agent.ShellChunk{{Data: "hi\n"}}, ExitCode: 0}}})
	want2 := "\x1b[1A\r\x1b[K▸ run_shell echo hi\n  hi\n  ↳ exit 0 · 0ms · 1 行\n"
	if got := out2.String(); got != want2 {
		t.Errorf("rich TTY 内联重绘应为基线字节\n got %q\nwant %q", got, want2)
	}
}
