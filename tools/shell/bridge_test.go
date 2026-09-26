package shell

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LaoQi/tanya/readline"
)

type fakeConsole struct {
	fullErr  error
	readEnd  *os.File
	full     bool
	released bool
	env      []string
}

type fakeLease struct {
	stdin    *os.File
	readEnd  *os.File
	onDone   chan struct{}
	released bool
	owner    *fakeConsole
}

func (l *fakeLease) Stdin() *os.File { return l.stdin }

func (l *fakeLease) Handover(pid int) bool {
	if l.stdin != nil {
		l.stdin.Close()
		l.stdin = nil
	}
	return false
}

func (l *fakeLease) Release() {
	if l.released {
		return
	}
	l.released = true
	if l.owner != nil {
		l.owner.released = true
	}
	if l.onDone != nil {
		<-l.onDone
	}
	if l.readEnd != nil {
		l.readEnd.Close()
		l.readEnd = nil
	}
}

func (f *fakeConsole) LendStdin() (Lease, error) { return &fakeLease{}, nil }

func (f *fakeConsole) LendFull(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	if f.fullErr != nil {
		return nil, f.fullErr
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	f.readEnd = r
	cmd.Stdout = w
	cmd.Stderr = w
	f.full = true
	f.env = cmd.Env
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(capture, r)
		close(done)
	}()
	return &fakeLease{stdin: w, readEnd: r, onDone: done, owner: f}, nil
}

func consoleTool(t *testing.T, c Console) *Tool {
	t.Helper()
	return testShellTool(t, func(cfg *Config) { cfg.Console = c })
}

type rlConsole struct{ con readline.Console }

func (c rlConsole) LendStdin() (Lease, error) {
	l, err := c.con.LendStdin()
	if l == nil {
		return nil, err
	}
	return l.(Lease), err
}

func (c rlConsole) LendFull(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	l, err := c.con.LendFull(cmd, capture)
	if l == nil {
		return nil, err
	}
	return l.(Lease), err
}

func TestRunShellFullCapture(t *testing.T) {
	f := &fakeConsole{}
	res := consoleTool(t, f).run(context.Background(), request{Command: `echo hi; echo oops >&2`, TimeoutSec: 10, Interactive: true})
	if !f.full || !f.released {
		t.Fatalf("借出未走全流程: %+v", f)
	}
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "hi") || !strings.Contains(out.String(), "oops") {
		t.Errorf("捕获流不完整: %q", out.String())
	}
	if len(res.Stderr) != 0 {
		t.Errorf("桥接路径为单流，stderr 应为空: %+v", res.Stderr)
	}
	if res.Err != "" || res.ExitCode != 0 {
		t.Errorf("结果异常: %+v", res)
	}
}

func TestRunShellLendFullFailureFallsBack(t *testing.T) {
	f := &fakeConsole{fullErr: errors.New("no tty")}
	res := consoleTool(t, f).run(context.Background(), request{Command: "echo fallback", TimeoutSec: 10, Interactive: true})
	if f.released {
		t.Fatal("借出失败仍进入 full 路径")
	}
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "fallback") {
		t.Errorf("回退路径未执行命令: %+v", res)
	}
}

func TestRunShellLendStdinNoFull(t *testing.T) {
	f := &fakeConsole{fullErr: errors.New("attach failed")}
	res := consoleTool(t, f).run(context.Background(), request{Command: "echo fallback2", TimeoutSec: 10, Interactive: true})
	if f.released {
		t.Fatal("full 借出失败应回退")
	}
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "fallback2") {
		t.Errorf("回退路径未执行命令: %+v", res)
	}
}

func TestRunShellNonInteractiveSkipsFull(t *testing.T) {
	f := &fakeConsole{}
	res := consoleTool(t, f).run(context.Background(), request{Command: "echo plain", TimeoutSec: 10})
	if f.full {
		t.Fatalf("非交互不应借用 full: %+v", f)
	}
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "plain") {
		t.Errorf("非交互路径异常: %+v", res)
	}
}

func TestRunShellNoConsoleInjected(t *testing.T) {
	res := testShellTool(t).run(context.Background(), request{Command: "echo plain", TimeoutSec: 10, Interactive: true})
	if res.Err != "" || res.ExitCode != 0 {
		t.Fatalf("无桥接注入应走现状路径: %+v", res)
	}
}

func TestRunShellFullRealTTYE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty: printf 'hello\\n' | script -qec 'TTY_E2E=1 go test -run TestRunShellFullRealTTYE2E -v ./tools/shell' /dev/null")
	}
	res := consoleTool(t, rlConsole{readline.NewConsole()}).run(context.Background(), request{Command: `read x < /dev/tty; echo got:$x; tty`, TimeoutSec: 15, Interactive: true})
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "got:hello") {
		t.Fatalf("真实 tty 桥接未读到输入: %+v", res)
	}
	if strings.Contains(out.String(), "not a tty") || strings.Contains(out.String(), "/dev/tty\n") {
		t.Fatalf("子进程 tty 非 pty: %q", out.String())
	}
}

func TestRunShellFullRealTTYReuse(t *testing.T) {
	if os.Getenv("TTY_E2E_REUSE") == "" {
		t.Skip("需真实 tty: (printf 'hello\\n'; sleep 3; printf 'world\\n'; sleep 3) | script -qec 'TTY_E2E_REUSE=1 go test -count=1 -run TestRunShellBridgedRealTTYReuse -v ./tools/shell' /dev/null")
	}
	for _, want := range []string{"hello", "world"} {
		res := consoleTool(t, rlConsole{readline.NewConsole()}).run(context.Background(), request{Command: `read -r x < /dev/tty; echo got:$x`, TimeoutSec: 15, Interactive: true})
		var out strings.Builder
		for _, c := range res.Stdout {
			out.WriteString(c.Data)
		}
		if !strings.Contains(out.String(), "got:"+want) {
			t.Fatalf("第 %q 次运行未读到输入: %+v", want, res)
		}
	}
}

func TestRunShellSignaledExitCode(t *testing.T) {
	res := testShellTool(t).run(context.Background(), request{Command: "kill -INT $$", TimeoutSec: 10})
	if res.ExitCode != 130 {
		t.Fatalf("SIGINT 应记为 130: %+v", res)
	}
	if res.Err != "" {
		t.Fatalf("信号终止不应记为 Err: %+v", res)
	}
}

func TestRunShellFullSignaledExitCode(t *testing.T) {
	res := consoleTool(t, &fakeConsole{}).run(context.Background(), request{Command: "kill -INT $$", TimeoutSec: 10, Interactive: true})
	if res.ExitCode != 130 {
		t.Fatalf("桥接路径 SIGINT 应记为 130: %+v", res)
	}
}

func TestRunShellFullCwdRealTTYE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty: printf '\\n' | script -qec 'TTY_E2E=1 go test -count=1 -run TestRunShellFullCwdRealTTYE2E -v ./tools/shell' /dev/null")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res := consoleTool(t, rlConsole{readline.NewConsole()}).run(context.Background(), request{Command: "pwd", TimeoutSec: 15, Interactive: true, Cwd: dir})
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(out.String()))
	if err != nil || got != dir {
		t.Fatalf("桥接下 cwd 未生效: got %q (%v) want %q; res=%+v", got, err, dir, res)
	}
	if res.Cwd != dir {
		t.Errorf("Cwd = %q want %q", res.Cwd, dir)
	}
}
