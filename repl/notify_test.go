package repl

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/tools/shell"
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
	o.Notify(Notification{Reason: NotifyNeedInput, Tool: "run\x1b]9;evil\a\nshell"})
	o.Notify(Notification{Reason: NotifyTurnDone, Duration: 3200 * time.Millisecond})
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
	o.Notify(Notification{Reason: NotifyNeedInput, Tool: strings.Repeat("宽", 300)})
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
	r.beginTurn(nil).End(nil)
	turn := r.beginTurn(nil)
	turn.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", Interactive: true})
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
	r.beginTurn(nil).End(nil)
	r.beginTurn(nil).Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", Interactive: true})
	if len(*got) != 0 {
		t.Errorf("plain 档不应写 OSC: %+v", *got)
	}
}

var testShellInv = shell.Invocation{Argv: []string{"/bin/bash", "-c"}, Kind: shell.KindPosix}

func captureCommandNotifier(t *testing.T, inv shell.Invocation, tmpl string) (*commandNotifier, *[][]string, chan struct{}) {
	t.Helper()
	c := mustCommandNotifier(t, inv, tmpl)
	var mu sync.Mutex
	var got [][]string
	done := make(chan struct{}, 8)
	c.run = func(_ context.Context, argv []string) error {
		mu.Lock()
		got = append(got, argv)
		mu.Unlock()
		done <- struct{}{}
		return nil
	}
	return c, &got, done
}

func mustCommandNotifier(t *testing.T, inv shell.Invocation, tmpl string) *commandNotifier {
	t.Helper()
	n, err := NewCommandNotifier(inv, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := n.(*commandNotifier)
	if !ok {
		t.Fatalf("应返回 *commandNotifier: %T", n)
	}
	return c
}

func waitNotify(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("通知命令未执行")
	}
}

func TestCommandNotifierArgv(t *testing.T) {
	c, got, done := captureCommandNotifier(t, testShellInv, "notify-send -a {title} {content}")
	c.Notify(Notification{Reason: NotifyTurnDone, Duration: 12*time.Second + 300*time.Millisecond, Failed: true})
	waitNotify(t, done)
	argv := (*got)[0]
	want := []string{"/bin/bash", "-c", "notify-send -a 'tanya' '回合失败 · 12.3s'"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv 不符:\n got %q\nwant %q", argv, want)
	}
}

func TestCommandNotifierKindPlaceholder(t *testing.T) {
	c, got, done := captureCommandNotifier(t, testShellInv, "hook {kind} {content}")
	c.Notify(Notification{Reason: NotifyNeedInput, Tool: "run_shell"})
	waitNotify(t, done)
	if argv := (*got)[0]; argv[len(argv)-1] != "hook 'input' 'run_shell 等待输入'" {
		t.Errorf("类别占位符不符: %q", argv[len(argv)-1])
	}
}

func TestCommandNotifierSingleFlight(t *testing.T) {
	c, _, _ := captureCommandNotifier(t, testShellInv, "hook {kind}")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls int32
	c.run = func(context.Context, []string) error {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(entered)
			<-release
		}
		return nil
	}
	c.Notify(Notification{Reason: NotifyTurnDone})
	<-entered
	c.Notify(Notification{Reason: NotifyTurnDone})
	c.Notify(Notification{Reason: NotifyNeedInput})
	time.Sleep(50 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("单飞应丢弃并发通知，实际执行 %d 次", n)
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&calls) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("首次结束后未释放单飞名额，调用数仍为 %d", atomic.LoadInt32(&calls))
		}
		c.Notify(Notification{Reason: NotifyTurnDone})
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCommandNotifierTimeout(t *testing.T) {
	c, _, _ := captureCommandNotifier(t, testShellInv, "hook {kind}")
	c.timeout = 20 * time.Millisecond
	seen := make(chan error, 1)
	c.run = func(ctx context.Context, _ []string) error {
		<-ctx.Done()
		seen <- ctx.Err()
		return ctx.Err()
	}
	c.Notify(Notification{Reason: NotifyTurnDone})
	select {
	case err := <-seen:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("超时应取消上下文: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("超时未生效")
	}
}

func TestCommandNotifierTemplateValidation(t *testing.T) {
	bad := []string{"notify-send {body}", "notify-send {title", "notify-send title}", `notify-send "{title}"`, "notify-send '{content}'"}
	for _, tmpl := range bad {
		if _, err := NewCommandNotifier(testShellInv, tmpl); err == nil {
			t.Errorf("模板应被拒绝: %q", tmpl)
		}
	}
	for _, tmpl := range []string{"notify-send {title} {content}", "notify-send --text=tanya:{content}", "hook {kind}"} {
		if _, err := NewCommandNotifier(testShellInv, tmpl); err != nil {
			t.Errorf("合法模板被拒绝 %q: %v", tmpl, err)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		kind shell.Kind
		in   string
		want string
	}{
		{shell.KindPosix, "it's ok", `'it'\''s ok'`},
		{shell.KindPowerShell, "it's ok", `'it''s ok'`},
		{shell.KindCmd, `say "hi"`, `"say ""hi"""`},
	}
	for _, c := range cases {
		if got := shellQuote(c.kind, c.in); got != c.want {
			t.Errorf("kind %v 引号不符: got %q want %q", c.kind, got, c.want)
		}
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
	n.Notify(Notification{Reason: NotifyTurnDone})
	for i, f := range []*fakeNotifier{a, b, c} {
		if len(f.got) != 1 {
			t.Errorf("第 %d 个行为应各收一条: %+v", i, f.got)
		}
	}
	if single := Notifiers(a); single != Notifier(a) {
		t.Errorf("单行为应原样返回: %T", single)
	}
}
