package repl

import "testing"

func TestAskSanesTerminalAndSubscribes(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, out, errb := newTestREPLAgent(t, a, term)
	r.ask("你好")
	if term.sane == 0 {
		t.Error("回合前应对终端做纯模式自愈（Console.Sane）")
	}
	if term.subs == 0 {
		t.Error("回合应订阅 Console 中断事件")
	}
	if errb.String() == "" {
		t.Errorf("单发失败应落错误行: out=%q err=%q", out.String(), errb.String())
	}
}
