package notify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LaoQi/battlelab/tools/shell"
)

var testShellInv = shell.Invocation{Argv: []string{"/bin/bash", "-c"}, Kind: shell.KindPosix}

func captureCommand(t *testing.T, inv shell.Invocation, tmpl string) (*Command, *[]string, chan struct{}) {
	t.Helper()
	c := mustCommand(t, inv, tmpl)
	var mu sync.Mutex
	var got []string
	done := make(chan struct{}, 8)
	c.run = func(_ context.Context, script string, _ shell.RunOptions) error {
		mu.Lock()
		got = append(got, script)
		mu.Unlock()
		done <- struct{}{}
		return nil
	}
	return c, &got, done
}

func mustCommand(t *testing.T, inv shell.Invocation, tmpl string) *Command {
	t.Helper()
	c, err := NewCommand(inv, nil, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newTestCommand(t *testing.T, tmpl string) (*Command, error) {
	t.Helper()
	return NewCommand(testShellInv, nil, tmpl)
}

func waitNotify(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("通知命令未执行")
	}
}

func TestCommandArgv(t *testing.T) {
	c, got, done := captureCommand(t, testShellInv, "notify-send -a {title} {content}")
	c.Notify(Notification{Title: "tanya", Content: "回合失败 · 12.3s", Kind: "failed"})
	waitNotify(t, done)
	want := "notify-send -a 'tanya' '回合失败 · 12.3s'"
	if script := (*got)[0]; script != want {
		t.Errorf("命令不符:\n got %q\nwant %q", script, want)
	}
}

func TestCommandKindPlaceholder(t *testing.T) {
	c, got, done := captureCommand(t, testShellInv, "hook {kind} {content}")
	c.Notify(Notification{Title: "tanya", Content: "run_shell 等待输入", Kind: "input"})
	waitNotify(t, done)
	if script := (*got)[0]; script != "hook 'input' 'run_shell 等待输入'" {
		t.Errorf("类别占位符不符: %q", script)
	}
}

func TestCommandSingleFlight(t *testing.T) {
	c, _, _ := captureCommand(t, testShellInv, "hook {kind}")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls int32
	c.run = func(context.Context, string, shell.RunOptions) error {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(entered)
			<-release
		}
		return nil
	}
	c.Notify(Notification{Kind: "done"})
	<-entered
	c.Notify(Notification{Kind: "done"})
	c.Notify(Notification{Kind: "input"})
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
		c.Notify(Notification{Kind: "done"})
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCommandTimeout(t *testing.T) {
	c, _, _ := captureCommand(t, testShellInv, "hook {kind}")
	c.timeout = 20 * time.Millisecond
	seen := make(chan error, 1)
	c.run = func(ctx context.Context, _ string, _ shell.RunOptions) error {
		<-ctx.Done()
		seen <- ctx.Err()
		return ctx.Err()
	}
	c.Notify(Notification{Kind: "done"})
	select {
	case err := <-seen:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("超时应取消上下文: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("超时未生效")
	}
}

func TestCommandTemplateValidation(t *testing.T) {
	bad := []string{"notify-send {body}", "notify-send {title", "notify-send title}", `notify-send "{title}"`, "notify-send '{content}'"}
	for _, tmpl := range bad {
		if _, err := newTestCommand(t, tmpl); err == nil {
			t.Errorf("模板应被拒绝: %q", tmpl)
		}
	}
	for _, tmpl := range []string{"notify-send {title} {content}", "notify-send --text=tanya:{content}", "hook {kind}"} {
		if _, err := newTestCommand(t, tmpl); err != nil {
			t.Errorf("合法模板被拒绝 %q: %v", tmpl, err)
		}
	}
}

func TestQuote(t *testing.T) {
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
		if got := Quote(c.kind, c.in); got != c.want {
			t.Errorf("kind %v 引号不符: got %q want %q", c.kind, got, c.want)
		}
	}
}

func TestCommandRunsThroughBuiltinShell(t *testing.T) {
	inv, err := shell.Resolve(shell.BuiltinShellName)
	if err != nil {
		t.Fatalf("解析内置解释器失败: %v", err)
	}
	if !inv.Builtin {
		t.Fatal("shell 配置为 vbash 时应解析为内置解释器")
	}
	dir := t.TempDir()
	c, err := NewCommand(inv, func() shell.RunOptions {
		return shell.RunOptions{Dir: dir, Env: []string{"PATH=" + os.Getenv("PATH")}}
	}, "printf 'ran:%s' {kind} > ran.txt")
	if err != nil {
		t.Fatal(err)
	}
	c.Notify(Notification{Title: "tanya", Content: "x", Kind: "done"})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "ran.txt")); err == nil {
			if strings.TrimSpace(string(b)) != "ran:done" {
				t.Errorf("内置解释器执行结果不符: %q", b)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("通知命令未在 3s 内经内置解释器执行")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCommandBuiltinNeedsNoHostShell(t *testing.T) {
	inv, err := shell.Resolve(shell.BuiltinShellName)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Argv) != 0 {
		t.Fatalf("内置解释器不应回落宿主 argv: %q", inv.Argv)
	}
	c, got, done := captureCommand(t, inv, "hook {kind}")
	c.Notify(Notification{Kind: "input"})
	waitNotify(t, done)
	if (*got)[0] != "hook 'input'" {
		t.Errorf("render 不符: %q", (*got)[0])
	}
}
