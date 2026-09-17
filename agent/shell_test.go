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
	got := runShellString(t, context.Background(), "echo hello", 10)
	if !strings.Contains(got, "stdout:\nhello") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellExitCode(t *testing.T) {
	got := runShellString(t, context.Background(), "exit 3", 10)
	if !strings.Contains(got, "exit code: 3") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellStderr(t *testing.T) {
	got := runShellString(t, context.Background(), "echo oops >&2", 10)
	if !strings.Contains(got, "stderr:\noops") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellNoOutput(t *testing.T) {
	got := runShellString(t, context.Background(), "true", 10)
	if !strings.Contains(got, "退出码 0") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellTimeout(t *testing.T) {
	got := runShellString(t, context.Background(), "sleep 5", 1)
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
	got := runShellString(t, ctx, "sleep 5", 60)
	if !strings.Contains(got, "已中断") || !strings.Contains(got, "输出可能不完整") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellInterruptNotStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := runShellString(t, ctx, "echo hi", 60)
	if !strings.Contains(got, "命令未执行") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "hi") {
		t.Errorf("命令不应执行: %q", got)
	}
}

func TestRunShellTimeoutClamp(t *testing.T) {
	got := runShellString(t, context.Background(), "echo ok", 9999)
	if !strings.Contains(got, "ok") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellTruncation(t *testing.T) {
	got := runShellString(t, context.Background(), "head -c 200000 /dev/zero | tr '\\0' 'a'", 30)
	if !strings.Contains(got, "中间截断 150000 字节") {
		t.Errorf("应包含中间截断标记: len=%d", len(got))
	}
	if len(got) > 2*shellMaxOutput+200 {
		t.Errorf("截断后仍过长: %d", len(got))
	}
}

func TestRunShellKillsProcessGroup(t *testing.T) {
	marker := fmt.Sprintf("tanya_pg_%d", os.Getpid())
	got := runShellString(t, context.Background(), "exec -a "+marker+" sleep 30 & wait", 1)
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
	got := runShellString(t, context.Background(), "sleep 30 & echo ok", 60)
	elapsed := time.Since(start)
	if !strings.Contains(got, "ok") {
		t.Errorf("got %q", got)
	}
	if elapsed > 10*time.Second {
		t.Errorf("WaitDelay 未生效，耗时 %v", elapsed)
	}
}

func TestLimitedBuffer(t *testing.T) {
	var chunks []ShellChunk
	c := streamCapture{chunks: &chunks}
	full := strings.Repeat("a", shellMaxOutput+500)
	if n, err := c.Write([]byte(full)); err != nil || n != len(full) {
		t.Fatalf("Write 返回 n=%d err=%v", n, err)
	}
	c.finish()
	if len(chunks) != 1 || len(chunks[0].Data) != shellMaxOutput+500 || chunks[0].Truncated != 0 {
		t.Fatalf("连续数据应合并为单一 chunk: %+v（len=%d）", chunks, len(chunks[0].Data))
	}
	var chunks2 []ShellChunk
	c2 := streamCapture{chunks: &chunks2}
	c2.Write([]byte("x"))
	c2.Write([]byte(strings.Repeat("a", shellMaxOutput*2)))
	c2.finish()
	if len(chunks2) != 2 {
		t.Fatalf("应产出 2 chunk，实际 %d", len(chunks2))
	}
	if len(chunks2[0].Data) != shellMaxOutput || chunks2[0].Truncated != 0 {
		t.Errorf("chunk[0] 异常: len=%d trunc=%d", len(chunks2[0].Data), chunks2[0].Truncated)
	}
	if len(chunks2[1].Data) != shellMaxOutput/2+1 || chunks2[1].Truncated != shellMaxOutput/2 {
		t.Errorf("chunk[1] 异常: len=%d trunc=%d", len(chunks2[1].Data), chunks2[1].Truncated)
	}
	var chunks3 []ShellChunk
	c3 := streamCapture{chunks: &chunks3}
	c3.finish()
	if len(chunks3) != 0 {
		t.Errorf("空流不应产出 chunk: %+v", chunks3)
	}
}

func TestShellArgsInteractive(t *testing.T) {
	ps := &shellProfile{Path: "pwsh", Name: "pwsh", Kind: KindPowerShell, ExtraArgs: []string{"-NoProfile", "-NonInteractive"}}
	if got := strings.Join(shellArgs(ps, "echo hi", false), " "); got != "-NoProfile -NonInteractive -Command echo hi" {
		t.Errorf("非交互应保留 -NonInteractive: %q", got)
	}
	if got := strings.Join(shellArgs(ps, "Read-Host x", true), " "); got != "-NoProfile -Command Read-Host x" {
		t.Errorf("交互应去除 -NonInteractive: %q", got)
	}
	cmdProfile := &shellProfile{Path: "cmd", Name: "cmd", Kind: KindCmd, ExtraArgs: []string{"/d", "/s"}}
	if got := strings.Join(shellArgs(cmdProfile, "dir", true), " "); got != "/d /s /c dir" {
		t.Errorf("cmd 参数不应受 interactive 影响: %q", got)
	}
}
