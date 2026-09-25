//go:build darwin

package shell

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPlatformDarwinSharesPosixBase(t *testing.T) {
	if got := strings.Join(platform.Candidates, "/"); got != "bash/sh/ash" {
		t.Errorf("darwin 候选链 = %q", got)
	}
	if len(platform.Programs) == 0 {
		t.Error("darwin 程序清单不应为空（与 posix 共享）")
	}
	if platform.Capabilities == nil || platform.KillGroup == nil {
		t.Errorf("darwin 平台能力存在缺项: %+v", platform)
	}
}

func TestDarwinProcessStopped(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动 sleep: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if _, err := unix.SysctlKinfoProc("kern.proc.pid", cmd.Process.Pid); err != nil {
		t.Skipf("kern.proc.pid 不可读: %v", err)
	}
	if darwinProcessStopped(cmd.Process.Pid) {
		t.Fatalf("运行中的进程不应判定为已停止: pid=%d", cmd.Process.Pid)
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGSTOP); err != nil {
		t.Skipf("SIGSTOP 发送失败: %v", err)
	}
	if !waitDarwinState(cmd.Process.Pid, true) {
		t.Fatalf("SIGSTOP 后未检出停止状态: pid=%d", cmd.Process.Pid)
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGCONT); err != nil {
		t.Skipf("SIGCONT 发送失败: %v", err)
	}
	if !waitDarwinState(cmd.Process.Pid, false) {
		t.Fatalf("SIGCONT 后仍判定为已停止: pid=%d", cmd.Process.Pid)
	}
}

func waitDarwinState(pid int, stopped bool) bool {
	deadline := time.Now().Add(3 * time.Second)
	for {
		if darwinProcessStopped(pid) == stopped {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDarwinProcessStoppedGone(t *testing.T) {
	if darwinProcessStopped(os.Getpid() + 1<<20) {
		t.Error("不存在的进程应判定为未停止")
	}
}
