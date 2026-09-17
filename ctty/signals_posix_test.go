//go:build linux || darwin

package ctty

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestWatchSignalsTable(t *testing.T) {
	got := watchSignals()
	want := []os.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestExitRecordsFirstSignal(t *testing.T) {
	withResetSignals(t)
	Exit(syscall.SIGTERM)
	if !Exiting() {
		t.Error("Exit 后应处于退出态")
	}
	if ExitSignal() != syscall.SIGTERM {
		t.Errorf("ExitSignal: %v", ExitSignal())
	}
	if ExitStatus() != 143 {
		t.Errorf("ExitStatus: %d", ExitStatus())
	}
	Exit(syscall.SIGHUP)
	if ExitSignal() != syscall.SIGTERM || ExitStatus() != 143 {
		t.Errorf("首个来源应生效: %v %d", ExitSignal(), ExitStatus())
	}
}

func TestDispatchExitSignal(t *testing.T) {
	withResetSignals(t)
	ch := make(chan os.Signal, 2)
	go dispatch(ch)
	t.Cleanup(func() { close(ch) })
	before := Interrupted()
	ch <- syscall.SIGTERM
	deadline := time.Now().Add(2 * time.Second)
	for !Exiting() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !Exiting() {
		t.Fatal("关闭信号应置退出态")
	}
	if ExitSignal() != syscall.SIGTERM || ExitStatus() != 143 {
		t.Errorf("首个来源应生效: %v %d", ExitSignal(), ExitStatus())
	}
	select {
	case <-before:
	default:
		t.Error("退出请求应同时广播中断")
	}
}

func TestRepeatExitSignalTerminates(t *testing.T) {
	if os.Getenv("CTTY_REPEAT_HELPER") == "1" {
		ch := make(chan os.Signal, 2)
		go dispatch(ch)
		ch <- syscall.SIGTERM
		ch <- syscall.SIGTERM
		select {}
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRepeatExitSignalTerminates")
	cmd.Env = append(os.Environ(), "CTTY_REPEAT_HELPER=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper 启动失败: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("重复信号应终止进程: %v", err)
		}
		if code := exitErr.ExitCode(); code != 143 {
			t.Errorf("退出码: got %d want 143", code)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("重复关闭信号未在 5s 内终止子进程")
	}
}
