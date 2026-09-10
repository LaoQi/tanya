//go:build linux

package readline

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func newTestPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	t.Cleanup(func() {
		master.Close()
		slave.Close()
	})
	return master, slave
}

func TestSecureTerminalEnablesISIG(t *testing.T) {
	_, slave := newTestPTY(t)
	fd := int(slave.Fd())
	term, err := getTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	term.Lflag &^= unix.ISIG
	if err := setTermios(fd, term); err != nil {
		t.Fatalf("setTermios: %v", err)
	}
	secureTerminalFd(fd, true)
	got, err := getTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	if got.Lflag&unix.ISIG == 0 {
		t.Fatalf("ISIG 未恢复: lflag=0x%x", got.Lflag)
	}
	if got.Lflag&unix.ICANON == 0 || got.Lflag&unix.ECHO == 0 {
		t.Errorf("其它 lflag 被误改: 0x%x", got.Lflag)
	}
}

func TestSecureTerminalAlreadyEnabled(t *testing.T) {
	_, slave := newTestPTY(t)
	fd := int(slave.Fd())
	before, err := getTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	before.Lflag |= unix.ISIG
	if err := setTermios(fd, before); err != nil {
		t.Fatalf("setTermios: %v", err)
	}
	secureTerminalFd(fd, true)
	got, err := getTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	if got.Lflag != before.Lflag {
		t.Fatalf("ISIG 已置位时不应改动 lflag: 0x%x -> 0x%x", before.Lflag, got.Lflag)
	}
}

func TestSecureTerminalSkipsWhenNotOwner(t *testing.T) {
	_, slave := newTestPTY(t)
	fd := int(slave.Fd())
	term, err := getTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	term.Lflag &^= unix.ISIG
	if err := setTermios(fd, term); err != nil {
		t.Fatalf("setTermios: %v", err)
	}
	secureTerminalFd(fd, false)
	got, err := getTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	if got.Lflag&unix.ISIG != 0 {
		t.Fatalf("非属主时不应改动 ISIG: lflag=0x%x", got.Lflag)
	}
}

func TestSecureTerminalReclaimsForeground(t *testing.T) {
	if os.Getenv("SECURE_TERMINAL_HELPER") == "1" {
		secureTerminalHelper()
		return
	}
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	cmd := exec.Command(os.Args[0], "-test.run=TestSecureTerminalReclaimsForeground")
	cmd.Env = append(os.Environ(), "SECURE_TERMINAL_HELPER=1")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper 启动失败: %v", err)
	}
	slave.Close()
	var out []byte
	buf := make([]byte, 256)
	for {
		n, err := master.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper 失败: %v out=%q", err, out)
	}
	if !strings.Contains(string(out), "RECLAIMED") {
		t.Fatalf("未夺回前台: %q", out)
	}
}

func secureTerminalHelper() {
	InitTerminalGuard()
	fd := int(os.Stdin.Fd())
	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		os.Stdout.WriteString("STARTFAIL\n")
		os.Exit(3)
	}
	defer func() { _ = child.Process.Kill() }()
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, child.Process.Pid); err != nil {
		os.Stdout.WriteString("SETFAIL " + err.Error() + "\n")
		os.Exit(4)
	}
	secureTerminalFd(fd, true)
	cur, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil {
		os.Stdout.WriteString("GETFAIL\n")
		os.Exit(5)
	}
	if cur != unix.Getpgrp() {
		os.Stdout.WriteString("NOTRECLAIMED\n")
		os.Exit(6)
	}
	os.Stdout.WriteString("RECLAIMED\n")
	os.Exit(0)
}
