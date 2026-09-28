package repl

import (
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/present"
	"github.com/LaoQi/tanya/render/term"
)

type fakeNotifier struct{ got []present.Notification }

func (f *fakeNotifier) Notify(n present.Notification) { f.got = append(f.got, n) }

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
	sink := notifySink{r: r}
	sink.Emit(agent.Event{Kind: agent.EventTurnEnd, Turn: agent.TurnInfo{Duration: 1500 * time.Millisecond}})
	if len(n.got) != 1 {
		t.Fatalf("成功回合应通知一次: %+v", n.got)
	}
	if got := n.got[0]; got.Kind != notifyKindDone || !strings.HasPrefix(got.Content, "回合结束 · ") {
		t.Errorf("载荷不符: %+v", got)
	}
	sink.Emit(agent.Event{Kind: agent.EventTurnEnd, Turn: agent.TurnInfo{Duration: 1500 * time.Millisecond, Failed: true}})
	if len(n.got) != 2 || n.got[1].Kind != notifyKindFailed {
		t.Errorf("报错回合同样通知且类别为 failed: %+v", n.got)
	}
	if !strings.HasPrefix(n.got[1].Content, "回合失败 · ") {
		t.Errorf("失败文案不符: %q", n.got[1].Content)
	}
}

func TestNotifyTurnDoneSkipsInterrupt(t *testing.T) {
	r, n := withBellREPL(t, modeRich, ttyRich())
	notifySink{r: r}.Emit(agent.Event{Kind: agent.EventTurnEnd, Turn: agent.TurnInfo{Duration: time.Second, Interrupted: true}})
	if len(n.got) != 0 {
		t.Errorf("中断（用户就在终端前）不应通知: %+v", n.got)
	}
}

func TestNotifyNeedInput(t *testing.T) {
	r, n := withBellREPL(t, modeRich, ttyRich())
	sink := notifySink{r: r}
	sink.Emit(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"sudo -S true"}`, Interactive: true})
	sink.Emit(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"ls"}`})
	if len(n.got) != 1 {
		t.Fatalf("只有 interactive 工具应通知: %+v", n.got)
	}
	if got := n.got[0]; got.Kind != notifyKindInput || got.Content != "run_shell 等待输入" {
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

// notifyPayload 走生产链路（payloadOf 成品化 → 行为）投递一次通知。
func notifyPayload(t *testing.T, n Notifier, in Notification) {
	t.Helper()
	p, ok := payloadOf(in)
	if !ok {
		t.Fatalf("载荷应可成品化: %+v", in)
	}
	n.Notify(p)
}

func oscCapture() (oscNotifier, *[]string) {
	var got []string
	return oscNotifier{write: func(s string) error {
		got = append(got, s)
		return nil
	}}, &got
}

// 载荷在我们的组装点就已是成品：单行、无控制序列、已截断，两条行为拿到的是同一份文本。
func TestNotifyPayloadSanitized(t *testing.T) {
	o, got := oscCapture()
	notifyPayload(t, o, Notification{Reason: NotifyNeedInput, Tool: "run\x1b]9;evil\a\nshell"})
	notifyPayload(t, o, Notification{Reason: NotifyTurnDone, Duration: 3200 * time.Millisecond})
	if len(*got) != 2 {
		t.Fatalf("应写入两条: %+v", *got)
	}
	first, second := (*got)[0], (*got)[1]
	if first != "tanya: run shell 等待输入" {
		t.Errorf("外部内容应被压成单行并剥掉控制序列: %q", first)
	}
	if second != "tanya: 回合结束 · 3.2s" {
		t.Errorf("成功回合文案不符: %q", second)
	}
	for _, s := range *got {
		if strings.ContainsAny(s, "\x1b\a\n\r\t") {
			t.Errorf("载荷不应含控制序列或换行: %q", s)
		}
	}
}

func TestNotifyPayloadTruncated(t *testing.T) {
	o, got := oscCapture()
	notifyPayload(t, o, Notification{Reason: NotifyNeedInput, Tool: strings.Repeat("宽", 300)})
	if len(*got) != 1 {
		t.Fatalf("应写入一条: %+v", *got)
	}
	body := strings.TrimPrefix((*got)[0], NotifyTitle+": ")
	if w := term.Width(body); w > notifyContentMax {
		t.Errorf("内容应截断到 %d 列内，实际 %d: %q", notifyContentMax, w, body)
	}
	if !strings.HasSuffix(body, "~") {
		t.Errorf("截断应带尾巴标记: %q", body)
	}
}

func TestNotifyOSCViaREPL(t *testing.T) {
	o, got := oscCapture()
	r, _, _ := newTestREPLMode(t, newFakeTerm(), modeRich, ttyRich())
	r.notifier = o
	t.Cleanup(r.view.Stop)
	sink := notifySink{r: r}
	sink.Emit(agent.Event{Kind: agent.EventTurnEnd, Turn: agent.TurnInfo{Duration: 1500 * time.Millisecond}})
	sink.Emit(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", Interactive: true})
	if len(*got) != 2 {
		t.Fatalf("回合结束与等待输入各一条: %+v", *got)
	}
	if !strings.HasPrefix((*got)[0], NotifyTitle+": 回合结束 · ") || (*got)[1] != "tanya: run_shell 等待输入" {
		t.Errorf("OSC 载荷不符: %+v", *got)
	}
}

func TestNotifyOSCGated(t *testing.T) {
	o, got := oscCapture()
	r, _, _ := newTestREPLMode(t, newFakeTerm(), modePlain, ttyRich())
	r.notifier = o
	t.Cleanup(r.view.Stop)
	sink := notifySink{r: r}
	sink.Emit(agent.Event{Kind: agent.EventTurnEnd, Turn: agent.TurnInfo{Duration: time.Second}})
	sink.Emit(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", Interactive: true})
	if len(*got) != 0 {
		t.Errorf("plain 档不应写 OSC: %+v", *got)
	}
}

func TestNotifiersFanOut(t *testing.T) {
	if n := Notifiers(nil, nil); n != nil {
		t.Errorf("全空应返回 nil: %T", n)
	}
	a, b := &fakeNotifier{}, &fakeNotifier{}
	c := &fakeNotifier{}
	n := Notifiers(nil, a, b, c)
	if n == nil {
		t.Fatal("有行为时不应为 nil")
	}
	n.Notify(present.Notification{Kind: notifyKindDone})
	for i, f := range []*fakeNotifier{a, b, c} {
		if len(f.got) != 1 {
			t.Errorf("第 %d 个行为应各收一条: %+v", i, f.got)
		}
	}
	if single := Notifiers(a); single != Notifier(a) {
		t.Errorf("单行为应原样返回: %T", single)
	}
}
