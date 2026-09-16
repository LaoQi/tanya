//go:build linux

package agent

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestStatState(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"12345 (bash) S 1 2 3 4 5 6 7", "S"},
		{"12345 (a b) T 1 2 3 4 5 6 7", "T"},
		{"12345 (bash) R", "R"},
		{"broken", ""},
		{"12345 ()", ""},
	}
	for _, c := range cases {
		if got := statState(c.in); got != c.want {
			t.Errorf("statState(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLinuxProcessStopped(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动 sleep: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if linuxProcessStopped(cmd.Process.Pid) {
		t.Fatalf("运行中的进程不应判定为已停止: pid=%d", cmd.Process.Pid)
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGSTOP); err != nil {
		t.Skipf("SIGSTOP 发送失败: %v", err)
	}
	if !waitState(t, cmd.Process.Pid, true) {
		t.Fatalf("SIGSTOP 后未检出 T 状态: pid=%d", cmd.Process.Pid)
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGCONT); err != nil {
		t.Skipf("SIGCONT 发送失败: %v", err)
	}
	if !waitState(t, cmd.Process.Pid, false) {
		t.Fatalf("SIGCONT 后仍判定为已停止: pid=%d", cmd.Process.Pid)
	}
}

func waitState(t *testing.T, pid int, stopped bool) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if linuxProcessStopped(pid) == stopped {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
