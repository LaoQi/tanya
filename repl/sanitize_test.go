package repl

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/term"
)

const evilText = "x\x1b[2J\x1b[1;31mred\x1b]0;pwned\x07\r\x1b[Kz"

func assertNoControl(t *testing.T, s, what string) {
	t.Helper()
	for _, bad := range []string{"\x1b", "\x07", "\r"} {
		if strings.Contains(s, bad) {
			t.Errorf("%s 残留控制字符 %q: %q", what, bad, s)
		}
	}
}

func assertNoEvil(t *testing.T, s, what string) {
	t.Helper()
	for _, bad := range []string{"\x1b[2J", "\x1b]0;", "\x07", "\r\x1b[K", "\x1b[1;31m"} {
		if strings.Contains(s, bad) {
			t.Errorf("%s 残留注入序列 %q: %q", what, bad, s)
		}
	}
}

func TestHistoryFullSanitizesUserText(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	r, buf, _ := newTestREPL(t, newFakeTerm())
	r.printHistoryFull(1, agent.Message{Role: "user", Content: evilText})
	out := buf.String()
	assertNoEvil(t, out, "/history user 正文")
	if !strings.Contains(out, "xredz") {
		t.Errorf("可见文本应保留: %q", out)
	}
}

func TestHistoryFullSanitizesToolCallArgs(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	var tc agent.ToolCall
	tc.ID = "1"
	tc.Type = "function"
	tc.Function.Name = "run_shell"
	tc.Function.Arguments = `{"command":"` + evilText + `"}`
	r, buf, _ := newTestREPL(t, newFakeTerm())
	r.printHistoryFull(2, agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}})
	out := buf.String()
	assertNoEvil(t, out, "/history 工具调用行")
	if !strings.Contains(out, "→ run_shell") || !strings.Contains(out, `{"command":"xredz"}`) {
		t.Errorf("工具调用行文本应保留: %q", out)
	}
}

func TestHistoryFullSanitizesPlainAssistant(t *testing.T) {
	_, tn, out, _ := newReasonTestTurn(t, modePlain, term.Profile{TTY: false, Colors: term.LevelNone}, false)
	tn.r.printRendered(evilText)
	assertNoControl(t, out.String(), "plain /history assistant 正文")
	if !strings.Contains(out.String(), "xredz") {
		t.Errorf("可见文本应保留: %q", out.String())
	}
}

func TestPlainStreamContentSanitized(t *testing.T) {
	_, tn, out, _ := newReasonTestTurn(t, modePlain, term.Profile{TTY: false, Colors: term.LevelNone}, false)
	tn.writeContent("前")
	tn.writeContent(evilText)
	tn.writeContent("后\n")
	got := out.String()
	assertNoControl(t, got, "plain 实时正文流")
	if got != "前xredz后\n" {
		t.Errorf("正文文本应逐字保留: %q", got)
	}
}

func TestErrLineSanitizesExternalText(t *testing.T) {
	got := errLine(errors.New(evilText))
	assertNoControl(t, got, "错误行")
	if !strings.HasPrefix(got, "错误: ") || !strings.HasSuffix(got, "\n") {
		t.Errorf("错误行格式不符: %q", got)
	}
	if !strings.Contains(got, "xredz") {
		t.Errorf("错误文本应保留: %q", got)
	}
}

func TestSanitizeKeepsWideTextIntact(t *testing.T) {
	_, tn, out, _ := newReasonTestTurn(t, modePlain, term.Profile{TTY: false, Colors: term.LevelNone}, false)
	tn.writeContent("中文正文 · emoji 🚀\t制表\t带换行\n")
	if got := out.String(); got != "中文正文 · emoji 🚀\t制表\t带换行\n" {
		t.Errorf("正常文本不应被改动: %q", got)
	}
}

// ask 旁路的清洗下沉在 toolView.Handle 的 EventContent 分支（REPL 模式该事件被 turn 拦截、不达此处）。
func TestToolViewHandleContentSanitizes(t *testing.T) {
	out, errb := &syncBuf{}, &syncBuf{}
	st := NewStreams(out, errb, modePlainVerbose)
	v := NewToolView(st, term.Profile{TTY: false, Colors: term.LevelNone}, testSem(), func() int { return 80 }, 8)
	v.Handle(agent.Event{Kind: agent.EventContent, Text: evilText})
	got := out.String()
	assertNoControl(t, got, "ask 旁路正文")
	if !strings.Contains(got, "xredz") {
		t.Errorf("正文文本应保留: %q", got)
	}
}

func TestREPLFailErrSanitizes(t *testing.T) {
	r, _, errb := newTestREPL(t, newFakeTerm())
	r.failErr(errors.New(evilText))
	r.failText(evilText)
	got := errb.String()
	assertNoControl(t, got, "failErr/failText 错误行")
	if strings.Count(got, "错误: xredz\n") != 2 {
		t.Errorf("两条标准错误行应清洗且文本保留: %q", got)
	}
}

func TestFailErrSanitizesExternalText(t *testing.T) {
	var errOut bytes.Buffer
	st := NewStreams(io.Discard, &errOut, modePlain)
	st.FailErr("\n", errors.New(evilText))
	got := errOut.String()
	assertNoControl(t, got, "FailErr 错误行")
	if !strings.HasPrefix(got, "\n错误: ") || !strings.HasSuffix(got, "\n") {
		t.Errorf("前缀与错误行格式不符: %q", got)
	}
	if !strings.Contains(got, "xredz") {
		t.Errorf("错误文本应保留: %q", got)
	}
}
