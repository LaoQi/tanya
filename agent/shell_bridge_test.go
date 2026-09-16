package agent

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

type fakeTTYBridge struct {
	prepareErr error
	attachErr  error
	readEnd    *os.File
	prepared   bool
	attached   bool
	stopped    bool
	env        []string
}

func (f *fakeTTYBridge) Prepare(cmd *exec.Cmd) (*os.File, error) {
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	f.readEnd = r
	cmd.Stdout = w
	cmd.Stderr = w
	f.prepared = true
	f.env = cmd.Env
	return w, nil
}

func (f *fakeTTYBridge) Attach(capture io.Writer) (func(), error) {
	if f.attachErr != nil {
		return nil, f.attachErr
	}
	f.attached = true
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(capture, f.readEnd)
		close(done)
	}()
	return func() {
		f.stopped = true
		<-done
		f.readEnd.Close()
	}, nil
}

func bridgeTool(t *testing.T, b TTYBridge) *shellTool {
	t.Helper()
	return testShellTool(t, func(c *shellToolConfig) { c.Bridge = b })
}

func TestRunShellBridgedCapture(t *testing.T) {
	f := &fakeTTYBridge{}
	res := bridgeTool(t, f).run(context.Background(), shellRequest{Command: `echo hi; echo oops >&2`, TimeoutSec: 10, Interactive: true})
	if !f.prepared || !f.attached || !f.stopped {
		t.Fatalf("桥接未走全流程: %+v", f)
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

func TestRunShellBridgePrepareFailureFallsBack(t *testing.T) {
	f := &fakeTTYBridge{prepareErr: errors.New("no tty")}
	res := bridgeTool(t, f).run(context.Background(), shellRequest{Command: "echo fallback", TimeoutSec: 10, Interactive: true})
	if f.attached {
		t.Fatal("Prepare 失败仍进入桥接")
	}
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "fallback") {
		t.Errorf("回退路径未执行命令: %+v", res)
	}
}

func TestRunShellBridgeAttachFailureFallsBack(t *testing.T) {
	f := &fakeTTYBridge{attachErr: errors.New("attach failed")}
	res := bridgeTool(t, f).run(context.Background(), shellRequest{Command: "echo fallback2", TimeoutSec: 10, Interactive: true})
	if f.attached {
		t.Fatal("Attach 失败应回退")
	}
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "fallback2") {
		t.Errorf("回退路径未执行命令: %+v", res)
	}
}

func TestRunShellNonInteractiveSkipsBridge(t *testing.T) {
	f := &fakeTTYBridge{}
	res := bridgeTool(t, f).run(context.Background(), shellRequest{Command: "echo plain", TimeoutSec: 10})
	if f.prepared || f.attached {
		t.Fatalf("非交互不应使用桥接: %+v", f)
	}
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "plain") {
		t.Errorf("非交互路径异常: %+v", res)
	}
}

func TestRunShellNoBridgeInjected(t *testing.T) {
	res := testShellTool(t).run(context.Background(), shellRequest{Command: "echo plain", TimeoutSec: 10, Interactive: true})
	if res.Err != "" || res.ExitCode != 0 {
		t.Fatalf("无桥接注入应走现状路径: %+v", res)
	}
}

func TestRunShellBridgedRealTTYE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty: printf 'hello\\n' | script -qec 'TTY_E2E=1 go test -run TestRunShellBridgedRealTTYE2E -v ./agent' /dev/null")
	}
	res := bridgeTool(t, readline.NewTTYBridge()).run(context.Background(), shellRequest{Command: `read x < /dev/tty; echo got:$x; tty`, TimeoutSec: 15, Interactive: true})
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

func TestRunShellBridgedRealTTYReuse(t *testing.T) {
	if os.Getenv("TTY_E2E_REUSE") == "" {
		t.Skip("需真实 tty: (printf 'hello\\n'; sleep 3; printf 'world\\n'; sleep 3) | script -qec 'TTY_E2E_REUSE=1 go test -count=1 -run TestRunShellBridgedRealTTYReuse -v ./agent' /dev/null")
	}
	for _, want := range []string{"hello", "world"} {
		res := bridgeTool(t, readline.NewTTYBridge()).run(context.Background(), shellRequest{Command: `read -r x < /dev/tty; echo got:$x`, TimeoutSec: 15, Interactive: true})
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
	res := testShellTool(t).run(context.Background(), shellRequest{Command: "kill -INT $$", TimeoutSec: 10})
	if res.ExitCode != 130 {
		t.Fatalf("SIGINT 应记为 130: %+v", res)
	}
	if res.Err != "" {
		t.Fatalf("信号终止不应记为 Err: %+v", res)
	}
}

func TestRunShellBridgedSignaledExitCode(t *testing.T) {
	res := bridgeTool(t, &fakeTTYBridge{}).run(context.Background(), shellRequest{Command: "kill -INT $$", TimeoutSec: 10, Interactive: true})
	if res.ExitCode != 130 {
		t.Fatalf("桥接路径 SIGINT 应记为 130: %+v", res)
	}
}

func TestRunShellBridgedCwdRealTTYE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty: printf '\\n' | script -qec 'TTY_E2E=1 go test -count=1 -run TestRunShellBridgedCwdRealTTYE2E -v ./agent' /dev/null")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res := bridgeTool(t, readline.NewTTYBridge()).run(context.Background(), shellRequest{Command: "pwd", TimeoutSec: 15, Interactive: true, Cwd: dir})
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
