package repl

import (
	"errors"
	"github.com/LaoQi/tanya/render/term"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
)

type fakeNotifier struct{ got []Notification }

func (f *fakeNotifier) Notify(n Notification) { f.got = append(f.got, n) }

func withBellREPL(t *testing.T, mode outMode, prof term.Profile) (*REPL, *fakeNotifier) {
	t.Helper()
	r, _, _ := newTestREPLMode(t, newFakeTerm(), mode, prof)
	n := &fakeNotifier{}
	r.notifier = n
	t.Cleanup(r.view.Stop)
	return r, n
}

func ttyRich() term.Profile { return term.Profile{TTY: true, Colors: term.Level16} }

func TestNotifyTurnDone(t *testing.T) {
	r, n := withBellREPL(t, modeRich, ttyRich())
	r.beginTurn(nil).End(nil)
	if len(n.got) != 1 {
		t.Fatalf("成功回合应通知一次: %+v", n.got)
	}
	if got := n.got[0]; got.Reason != NotifyTurnDone || got.Failed {
		t.Errorf("载荷不符: %+v", got)
	}
	r.beginTurn(nil).End(errors.New("boom"))
	if len(n.got) != 2 || !n.got[1].Failed {
		t.Errorf("报错回合同样通知且 Failed 为真: %+v", n.got)
	}
	if d := n.got[1].Duration; d <= 0 || d > time.Minute {
		t.Errorf("耗时应为本次回合的正值: %v", d)
	}
}

func TestNotifyTurnDoneSkipsInterrupt(t *testing.T) {
	r, n := withBellREPL(t, modeRich, ttyRich())
	r.beginTurn(nil).End(&agent.InterruptError{})
	r.beginTurn(nil).End(&agent.InterruptError{Kept: true})
	if len(n.got) != 0 {
		t.Errorf("中断（用户就在终端前）不应通知: %+v", n.got)
	}
}

func TestNotifyNeedInput(t *testing.T) {
	r, n := withBellREPL(t, modeRich, ttyRich())
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"sudo -S true"}`, Interactive: true})
	turn.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"ls"}`})
	if len(n.got) != 1 {
		t.Fatalf("只有 interactive 工具应通知: %+v", n.got)
	}
	if got := n.got[0]; got.Reason != NotifyNeedInput || got.Tool != "run_shell" {
		t.Errorf("载荷不符: %+v", got)
	}
}

func TestNotifyGates(t *testing.T) {
	cases := []struct {
		name string
		mode outMode
		prof term.Profile
	}{
		{"非 TTY", modeRich, term.Profile{TTY: false, Colors: term.LevelNone}},
		{"plain 档", modePlain, ttyRich()},
		{"plain+verbose 档", modePlainVerbose, ttyRich()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, n := withBellREPL(t, c.mode, c.prof)
			r.beginTurn(nil).End(nil)
			turn := r.beginTurn(nil)
			turn.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", Interactive: true})
			if len(n.got) != 0 {
				t.Errorf("门禁外不应通知: %+v", n.got)
			}
		})
	}
}

// 未装配（bell: false）时 notifier 为 nil：两条触发路径照常执行且不炸，即"零开销降级"。
func TestNotifyWithoutNotifier(t *testing.T) {
	r, _, _ := newTestREPLMode(t, newFakeTerm(), modeRich, ttyRich())
	if r.notifier != nil {
		t.Fatalf("未装配时 notifier 应为 nil: %T", r.notifier)
	}
	r.beginTurn(nil).End(nil)
	r.beginTurn(nil).Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", Interactive: true})
}

// 分发层：斜杠命令回合与空行不走 turn.End（走 turnSep 旁路），不产生通知。
func TestNotifySlashCommandQuiet(t *testing.T) {
	dev := newFakeTerm(line("/help"), line(""), line("exit"))
	r, _, _ := newTestREPLMode(t, dev, modeRich, ttyRich())
	n := &fakeNotifier{}
	r.notifier = n
	t.Cleanup(r.view.Stop)
	if err := r.Run(); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 0 {
		t.Errorf("斜杠命令与空行不应通知: %+v", n.got)
	}
}
