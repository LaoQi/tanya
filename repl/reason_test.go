package repl

import (
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/term"
)

func newReasonTestTurn(t *testing.T, mode outMode, prof term.Profile, on bool) (*REPL, *turn, *syncBuf, *syncBuf) {
	t.Helper()
	r, out, errb := newTestREPLMode(t, newFakeTerm(), mode, prof)
	r.showReasoning = on
	return r, r.beginTurn(nil), out, errb
}

func reasonProfile() term.Profile { return term.Profile{TTY: true, Colors: term.LevelNone} }

func TestReasonSep(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	sem := testSem()
	if got := term.Strip(reasonSep(sem, MsgReasonHead, 0)); got != "─── 思考 ───\n" {
		t.Errorf("上分隔 = %q", got)
	}
	if got := term.Strip(reasonSep(sem, MsgReasonTail, 1500*time.Millisecond)); got != "─── 思考结束 · 1.5s ───\n" {
		t.Errorf("下分隔 = %q", got)
	}
	if got, want := reasonSep(sem, MsgReasonHead, 0), sem.Think.Sprint("─── 思考 ───")+"\n"; got != want {
		t.Errorf("分隔符应取 Think 语义色: got %q want %q", got, want)
	}
}

func TestReasoningRendersWithSeparators(t *testing.T) {
	_, tn, out, _ := newReasonTestTurn(t, modeRich, reasonProfile(), true)
	tn.Handle(agent.Event{Kind: agent.EventRequestStart})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "先想一步\n"})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "再想一步\n"})
	tn.Handle(agent.Event{Kind: agent.EventContent, Text: "答案\n"})
	tn.End(nil)

	got := term.Strip(out.String())
	if n := strings.Count(got, "─── 思考 ───\n"); n != 1 {
		t.Errorf("上分隔应恰好一次: %d\n%q", n, got)
	}
	if strings.Contains(got, MsgStatusThinking) {
		t.Errorf("打开思维链后不应有「思考中」状态行: %q", got)
	}
	endAt := strings.Index(got, "─── 思考结束 · ")
	if endAt < 0 {
		t.Fatalf("缺下分隔: %q", got)
	}
	if !strings.HasPrefix(got[endAt:], "─── 思考结束 · ") || !strings.Contains(got[endAt:], " ───\n") {
		t.Errorf("下分隔形态异常: %q", got[endAt:])
	}
	if thinkAt := strings.Index(got, "先想一步"); thinkAt < 0 || thinkAt > endAt {
		t.Errorf("思维链内容应在上分隔之后、下分隔之前: %q", got)
	}
	if ansAt := strings.Index(got, "答案"); ansAt < endAt {
		t.Errorf("正文应在下分隔之后: %q", got)
	}
	if !strings.Contains(got, "再想一步") {
		t.Errorf("第二个 delta 丢失: %q", got)
	}
	assertNoCursorControl(t, out.String())
}

func TestReasoningOffKeepsThinkingPhase(t *testing.T) {
	_, tn, out, _ := newReasonTestTurn(t, modeRich, reasonProfile(), false)
	tn.Handle(agent.Event{Kind: agent.EventRequestStart})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "想\n"})
	waitUntil(t, "思考相位", func() bool {
		return strings.Contains(term.Strip(out.String()), MsgStatusThinking+" ")
	})
	tn.End(nil)
	got := term.Strip(out.String())
	if strings.Contains(got, "─── 思考") {
		t.Errorf("关闭时不应有分隔符: %q", got)
	}
	if strings.Contains(got, "想") {
		t.Errorf("关闭时思维链文本不应上屏: %q", got)
	}
}

func TestReasoningGateBlocksPlain(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode outMode
		prof term.Profile
	}{
		{"plain", modePlain, reasonProfile()},
		{"plain+verbose", modePlainVerbose, reasonProfile()},
		{"非终端", modeRich, term.Profile{TTY: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, tn, out, _ := newReasonTestTurn(t, tc.mode, tc.prof, true)
			tn.Handle(agent.Event{Kind: agent.EventRequestStart})
			tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "思考文本\n"})
			tn.Handle(agent.Event{Kind: agent.EventContent, Text: "答案\n"})
			tn.End(nil)
			got := out.String()
			if strings.Contains(got, "思考") || strings.Contains(got, reasonRuleLeft) {
				t.Errorf("门禁外不得输出思维链: %q", got)
			}
		})
	}
}

func TestReasoningSegmentsReopen(t *testing.T) {
	_, tn, out, _ := newReasonTestTurn(t, modeRich, reasonProfile(), true)
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "第一段\n"})
	tn.Handle(agent.Event{Kind: agent.EventContent, Text: "中间正文\n"})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "第二段\n"})
	tn.End(nil)
	got := term.Strip(out.String())
	if n := strings.Count(got, "─── 思考 ───\n"); n != 2 {
		t.Errorf("中间出正文后应重开一段: %d\n%q", n, got)
	}
	if n := strings.Count(got, "─── 思考结束 · "); n != 2 {
		t.Errorf("每段都应收尾: %d\n%q", n, got)
	}
	if !strings.Contains(got, "第二段") {
		t.Errorf("第二段内容丢失: %q", got)
	}
}

func TestReasoningStopsWaitingHeartbeat(t *testing.T) {
	_, tn, out, _ := newReasonTestTurn(t, modeRich, reasonProfile(), true)
	tn.Handle(agent.Event{Kind: agent.EventRequestStart})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "思考\n"})
	got := term.Strip(out.String())
	if !strings.Contains(got, MsgStatusWaiting+" 0s \n") {
		t.Errorf("等待行应被收尾成独立一行: %q", got)
	}
	idx := strings.Index(got, "─── 思考")
	if idx < 0 {
		t.Fatalf("缺上分隔: %q", got)
	}
	if strings.Contains(got[idx:], ".") {
		t.Errorf("思维链期间不应继续追加心跳点: %q", got[idx:])
	}
	tn.End(nil)
}

func TestHandleCommandReasoning(t *testing.T) {
	r, out, errb := newTestREPLMode(t, newFakeTerm(), modeRich, reasonProfile())
	run := func(cmd string) string {
		out.Reset()
		errb.Reset()
		r.handleCommand(cmd)
		return out.String()
	}
	if got := run("/reasoning"); got != "思维链显示: 关\n" {
		t.Errorf("默认 = %q", got)
	}
	if got := run("/reasoning on"); got != MsgReasoningOn || !r.showReasoning {
		t.Errorf("开启 = %q", got)
	}
	if got := run("/reasoning"); got != "思维链显示: 开\n" {
		t.Errorf("开启后查看 = %q", got)
	}
	if got := run("/reasoning off"); got != MsgReasoningOff || r.showReasoning {
		t.Errorf("关闭 = %q", got)
	}
	run("/reasoning bogus")
	if !strings.Contains(errb.String(), "无效参数") || r.showReasoning {
		t.Errorf("非法参数应报错且不改状态: %q %v", errb.String(), r.showReasoning)
	}
}
