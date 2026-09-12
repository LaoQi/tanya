//go:build linux

package agent

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRunShellForegroundTTY(t *testing.T) {
	ProtectTerminalSignals()
	probe, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skip("无控制终端")
	}
	cur, err := unix.IoctlGetInt(int(probe.Fd()), unix.TIOCGPGRP)
	probe.Close()
	if err != nil {
		t.Skipf("TIOCGPGRP: %v", err)
	}
	if cur != syscall.Getpgrp() {
		t.Skip("当前进程组非前台（嵌套/后台环境）")
	}
	res := RunShellResult(context.Background(),
		`sleep 0.2; read -r _ _ _ _ pgrp _ _ tpgid _ < /proc/self/stat; [ "$pgrp" = "$tpgid" ] && echo FG-OK || echo FG-FAIL`, 10, false, "", "")
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "FG-OK") {
		t.Fatalf("前台交接未生效: %+v", res)
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	pgrp, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCGPGRP)
	if err != nil {
		t.Fatal(err)
	}
	if pgrp != syscall.Getpgrp() {
		t.Fatalf("前台组未归还: %d != %d", pgrp, syscall.Getpgrp())
	}
}

func TestRunShellStdinRead(t *testing.T) {
	if os.Getenv("TTY_FEED") == "" {
		t.Skip("需 script 喂入输入: printf 'secret\\n' | script -qec \"TTY_FEED=1 go test -run TestRunShellStdinRead -v ./agent\" /dev/null")
	}
	ProtectTerminalSignals()
	probe, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skip("无控制终端")
	}
	cur, err := unix.IoctlGetInt(int(probe.Fd()), unix.TIOCGPGRP)
	probe.Close()
	if err != nil {
		t.Skipf("TIOCGPGRP: %v", err)
	}
	if cur != syscall.Getpgrp() {
		t.Skip("当前进程组非前台（嵌套/后台环境）")
	}
	res := RunShellResult(context.Background(), `read -r -t 5 line; echo "RC=$? GOT=$line"`, 10, false, "", "")
	var out strings.Builder
	for _, c := range res.Stdout {
		out.WriteString(c.Data)
	}
	if !strings.Contains(out.String(), "RC=0") || !strings.Contains(out.String(), "GOT=secret") {
		t.Fatalf("stdin 未从 tty 读到输入: %+v", res)
	}
}

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

func TestShellResultStoppedString(t *testing.T) {
	r := &ShellResult{Stopped: true}
	if s := r.String(); !strings.Contains(s, "挂起") {
		t.Errorf("Stopped 未渲染: %q", s)
	}
}

func TestRunShellStopDetection(t *testing.T) {
	ProtectTerminalSignals()
	probe, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skip("无控制终端")
	}
	cur, err := unix.IoctlGetInt(int(probe.Fd()), unix.TIOCGPGRP)
	probe.Close()
	if err != nil {
		t.Skipf("TIOCGPGRP: %v", err)
	}
	if cur != syscall.Getpgrp() {
		t.Skip("当前进程组非前台（嵌套/后台环境）")
	}
	res := RunShellResult(context.Background(), `kill -TSTP $$`, 10, false, "", "")
	if !res.Stopped {
		t.Fatalf("未检测到挂起: %+v", res)
	}
	if res.Duration >= 3*time.Second {
		t.Fatalf("挂起探测过慢: %v", res.Duration)
	}
}
