package shell

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LaoQi/tanya/render/present"
)

var testShellInv = Invocation{Argv: []string{"/bin/bash", "-c"}, Kind: KindPosix}

func captureCommandNotifier(t *testing.T, inv Invocation, tmpl string) (*CommandNotifier, *[][]string, chan struct{}) {
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

func mustCommandNotifier(t *testing.T, inv Invocation, tmpl string) *CommandNotifier {
	t.Helper()
	c, err := NewCommandNotifier(inv, tmpl)
	if err != nil {
		t.Fatal(err)
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
	c.Notify(present.Notification{Title: "tanya", Content: "回合失败 · 12.3s", Kind: "failed"})
	waitNotify(t, done)
	argv := (*got)[0]
	want := []string{"/bin/bash", "-c", "notify-send -a 'tanya' '回合失败 · 12.3s'"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv 不符:\n got %q\nwant %q", argv, want)
	}
}

func TestCommandNotifierKindPlaceholder(t *testing.T) {
	c, got, done := captureCommandNotifier(t, testShellInv, "hook {kind} {content}")
	c.Notify(present.Notification{Title: "tanya", Content: "run_shell 等待输入", Kind: "input"})
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
	c.Notify(present.Notification{Kind: "done"})
	<-entered
	c.Notify(present.Notification{Kind: "done"})
	c.Notify(present.Notification{Kind: "input"})
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
		c.Notify(present.Notification{Kind: "done"})
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
	c.Notify(present.Notification{Kind: "done"})
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

func TestQuote(t *testing.T) {
	cases := []struct {
		kind Kind
		in   string
		want string
	}{
		{KindPosix, "it's ok", `'it'\''s ok'`},
		{KindPowerShell, "it's ok", `'it''s ok'`},
		{KindCmd, `say "hi"`, `"say ""hi"""`},
	}
	for _, c := range cases {
		if got := Quote(c.kind, c.in); got != c.want {
			t.Errorf("kind %v 引号不符: got %q want %q", c.kind, got, c.want)
		}
	}
}
