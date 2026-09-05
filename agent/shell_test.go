package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunShellStdout(t *testing.T) {
	got := RunShell(context.Background(), "echo hello", 10)
	if !strings.Contains(got, "stdout:\nhello") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellExitCode(t *testing.T) {
	got := RunShell(context.Background(), "exit 3", 10)
	if !strings.Contains(got, "exit code: 3") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellStderr(t *testing.T) {
	got := RunShell(context.Background(), "echo oops >&2", 10)
	if !strings.Contains(got, "stderr:\noops") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellNoOutput(t *testing.T) {
	got := RunShell(context.Background(), "true", 10)
	if !strings.Contains(got, "退出码 0") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellTimeout(t *testing.T) {
	got := RunShell(context.Background(), "sleep 5", 1)
	if !strings.Contains(got, "超时") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellInterrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	got := RunShell(ctx, "sleep 5", 60)
	if !strings.Contains(got, "已中断") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellTimeoutClamp(t *testing.T) {
	got := RunShell(context.Background(), "echo ok", 9999)
	if !strings.Contains(got, "ok") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellTruncation(t *testing.T) {
	got := RunShell(context.Background(), "head -c 40000 /dev/zero | tr '\\0' 'a'", 30)
	if !strings.Contains(got, "截断") {
		t.Errorf("应包含截断标记: len=%d", len(got))
	}
	if len(got) > shellMaxOutput+200 {
		t.Errorf("截断后仍过长: %d", len(got))
	}
}

func TestRunShellKillsProcessGroup(t *testing.T) {
	marker := fmt.Sprintf("tanyan_pg_%d", os.Getpid())
	got := RunShell(context.Background(), "exec -a "+marker+" sleep 30 & wait", 1)
	if !strings.Contains(got, "超时") {
		t.Fatalf("got %q", got)
	}
	time.Sleep(200 * time.Millisecond)
	if out, err := exec.Command("pgrep", "-f", marker).Output(); err == nil && len(out) > 0 {
		t.Errorf("子进程未被组杀: pgrep -f %s => %s", marker, out)
	}
}

func TestRunShellWaitDelay(t *testing.T) {
	start := time.Now()
	got := RunShell(context.Background(), "sleep 30 & echo ok", 60)
	elapsed := time.Since(start)
	if !strings.Contains(got, "ok") {
		t.Errorf("got %q", got)
	}
	if elapsed > 10*time.Second {
		t.Errorf("WaitDelay 未生效，耗时 %v", elapsed)
	}
}

func TestLimitedBuffer(t *testing.T) {
	var b limitedBuffer
	full := strings.Repeat("a", shellMaxOutput+500)
	if n, err := b.Write([]byte(full)); err != nil || n != len(full) {
		t.Fatalf("Write 返回 n=%d err=%v", n, err)
	}
	if len(b.buf) != shellMaxOutput {
		t.Errorf("缓冲应封顶 %d，实际 %d", shellMaxOutput, len(b.buf))
	}
	if b.dropped != 500 {
		t.Errorf("丢弃计数应 500，实际 %d", b.dropped)
	}
	var sb strings.Builder
	b.report(&sb, "stdout")
	out := sb.String()
	if !strings.Contains(out, "截断 500 字节") {
		t.Errorf("got %q", out)
	}
	if strings.Count(out, "a") != shellMaxOutput {
		t.Errorf("输出长度应 %d", shellMaxOutput)
	}
}
