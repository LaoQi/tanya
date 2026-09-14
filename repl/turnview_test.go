package repl

import (
	"errors"
	"github.com/LaoQi/tanyan/render/term"
	"github.com/LaoQi/tanyan/render/theme"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanyan/agent"
)

func ttyProfile(t *testing.T, p term.Profile) {
	t.Helper()
	old := term.GetProfile()
	term.SetProfile(p)
	t.Cleanup(func() { term.SetProfile(old) })
}

func TestTurnSepTimeOnly(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	out := turnSep(term.Profile{TTY: true, Colors: term.Level16}, testSem(), 0)
	plain := term.Strip(out)
	if !regexp.MustCompile(`^\n──── \d{2}:\d{2}:\d{2}\n$`).MatchString(plain) {
		t.Errorf("回合分隔线格式不符: %q", plain)
	}
	if !strings.HasPrefix(out, "\n\x1b[32m") || !strings.HasSuffix(out, "\x1b[0m\n") {
		t.Errorf("回合分隔线应整体 Ok 包裹且有前导尾随换行: %q", out)
	}
	if strings.Contains(plain, "回合") {
		t.Errorf("无耗时时不应出现耗时字段: %q", plain)
	}
}

func TestTurnSepWithDuration(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	plain := term.Strip(turnSep(term.Profile{TTY: true, Colors: term.Level16}, testSem(), 12*time.Second+400*time.Millisecond))
	if !regexp.MustCompile(`^\n──── \d{2}:\d{2}:\d{2} · 回合 12\.4s\n$`).MatchString(plain) {
		t.Errorf("带耗时分隔线格式不符: %q", plain)
	}
}

func TestTurnSepNoColor(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.LevelNone})
	out := turnSep(term.Profile{TTY: true, Colors: term.LevelNone}, testSem(), time.Second)
	if strings.Contains(out, "\x1b[") {
		t.Errorf("无色环境不应出现 SGR: %q", out)
	}
	if !regexp.MustCompile(`^\n──── \d{2}:\d{2}:\d{2} · 回合 1\.0s\n$`).MatchString(out) {
		t.Errorf("无色环境文本格式不符: %q", out)
	}
}

func TestTurnSepNonTTYBypass(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: false, Colors: term.LevelNone})
	nonTTY := term.Profile{TTY: false, Colors: term.LevelNone}
	if out := turnSep(nonTTY, testSem(), 0); out != "" {
		t.Errorf("非 TTY 不应打印分隔线: %q", out)
	}
	if out := turnSep(nonTTY, testSem(), 3*time.Second); out != "" {
		t.Errorf("非 TTY 不应打印带耗时分隔线: %q", out)
	}
}

func TestTurnDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0ms"},
		{900 * time.Millisecond, "900ms"},
		{time.Second, "1.0s"},
		{12*time.Second + 400*time.Millisecond, "12.4s"},
		{63 * time.Second, "1m03s"},
		{754 * time.Second, "12m34s"},
		{time.Hour + 2*time.Minute, "1h02m"},
	}
	for _, c := range cases {
		if got := turnDuration(c.in); got != c.want {
			t.Errorf("turnDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTurnGapOnce(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	r, buf, _ := newTestREPL(t, newFakeTerm())
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventReasoning})
	turn.Handle(agent.Event{Kind: agent.EventReasoning})
	if buf.String() != "\n" {
		t.Errorf("首个事件前应恰好补一个空行且无其他输出: %q", buf.String())
	}
	turn.Handle(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"echo hi"}`,
		Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "echo hi", ExitCode: 0}}})
	if !strings.Contains(buf.String(), "▸ run_shell") {
		t.Errorf("事件应送达工具视图: %q", buf.String())
	}
}

func TestTurnNonTTYNoGap(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: false, Colors: term.LevelNone})
	r, buf, _ := newTestREPL(t, newFakeTerm())
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventReasoning})
	turn.Handle(agent.Event{Kind: agent.EventReasoning})
	if buf.String() != "" {
		t.Errorf("非 TTY 不应补空行: %q", buf.String())
	}
}

func TestTurnNoGapOnZeroEventTurn(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	r, buf, _ := newTestREPL(t, newFakeTerm())
	r.beginTurn(nil).End(errors.New("立即失败"))
	if n := strings.Count(buf.String(), "\n"); n != 2 {
		t.Errorf("零事件回合只应有分隔线自身的两个换行（补空行会与它叠成双空行），实际 %d 个: %q", n, buf.String())
	}
}

func TestFlowContextCarriers(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	r, _, _ := newTestREPL(t, newFakeTerm())
	t1 := r.beginTurn(nil)
	if !t1.f.prof.TTY || t1.f.md == nil || t1.f.st != r.st {
		t.Errorf("flow 应携带 prof/md/st: %+v", t1.f)
	}
	t2 := r.beginTurn(nil)
	if t1.f.md == t2.f.md {
		t.Error("每回合应派发独立的 markdown 缓冲")
	}
	if !t2.f.mdEnabled() {
		t.Error("TTY + rich 下 mdEnabled 应为真")
	}
	s, ok := theme.Lookup("vivid")
	if !ok {
		t.Fatal("缺少 vivid 主题")
	}
	r.applyTheme(s)
	t3 := r.beginTurn(nil)
	if t3.f.rend != r.rend {
		t.Error("回合应取当前渲染器（/theme 后同步）")
	}
}

func TestTurnEndSettlesMarkdown(t *testing.T) {
	withPlainProfile(t)
	r, buf, _ := newTestREPL(t, newFakeTerm())
	turn := r.beginTurn(nil)
	turn.f.md.Write("滞留尾行")
	if buf.String() != "" {
		t.Errorf("End 之前不应输出尾行: %q", buf.String())
	}
	turn.End(nil)
	if !strings.Contains(buf.String(), "滞留尾行") {
		t.Errorf("End 应结算 markdown 尾行: %q", buf.String())
	}
}

func TestTurnInterruptAndErrorPaths(t *testing.T) {
	withPlainProfile(t)
	r, out, errb := newTestREPL(t, newFakeTerm())
	r.beginTurn(nil).End(&agent.InterruptError{})
	if !strings.Contains(errb.String(), MsgInterruptBare) {
		t.Errorf("裸中断应写 stderr: %q", errb.String())
	}
	r2, _, errb2 := newTestREPL(t, newFakeTerm())
	r2.beginTurn(nil).End(&agent.InterruptError{Kept: true})
	if !strings.Contains(errb2.String(), MsgInterruptKept) {
		t.Errorf("保留式中断应写 stderr: %q", errb2.String())
	}
	r3, out3, errb3 := newTestREPL(t, newFakeTerm())
	r3.beginTurn(nil).End(errors.New("boom"))
	if !strings.Contains(errb3.String(), "错误: boom") {
		t.Errorf("普通错误应写 stderr: %q", errb3.String())
	}
	if out.String() != "" || out3.String() != "" {
		t.Errorf("错误不应写 stdout: %q / %q", out.String(), out3.String())
	}
}
