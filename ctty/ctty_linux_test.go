//go:build linux

package ctty

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSupportedOnLinux(t *testing.T) {
	if !Supported {
		t.Error("linux 上 Supported 应为 true")
	}
}

func TestOwnPgrpMatchesProcess(t *testing.T) {
	if got, want := OwnPgrp(), syscall.Getpgrp(); got != want {
		t.Errorf("OwnPgrp() = %d, want %d", got, want)
	}
}

func TestInvalidFd(t *testing.T) {
	if _, ok := ForegroundPgrp(-1); ok {
		t.Error("ForegroundPgrp 非法 fd 应返回 false")
	}
	if SetForeground(-1, OwnPgrp()) {
		t.Error("SetForeground 非法 fd 应返回 false")
	}
	if IsForeground(-1) {
		t.Error("IsForeground 非法 fd 应判非前台")
	}
}

func TestIgnoreJobSignalsIdempotent(t *testing.T) {
	IgnoreJobSignals()
	IgnoreJobSignals()
}

func TestOpenWithoutTTY(t *testing.T) {
	f, err := Open()
	if err != nil {
		t.Skipf("无控制终端: %v", err)
	}
	defer f.Close()
	if pgrp, ok := ForegroundPgrp(int(f.Fd())); ok && pgrp <= 0 {
		t.Errorf("前台组应为正数: %d", pgrp)
	}
}

func TestForegroundRoundtrip(t *testing.T) {
	if os.Getenv("CTTY_FG_HELPER") == "1" {
		fgHelper()
		return
	}
	master, slave, err := openTestPTY(t)
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	cmd := exec.Command(os.Args[0], "-test.run=TestForegroundRoundtrip")
	cmd.Env = append(os.Environ(), "CTTY_FG_HELPER=1")
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
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("前台组往返失败: %q", out)
	}
}

func fgHelper() {
	IgnoreJobSignals()
	fd := int(os.Stdin.Fd())
	if !IsForeground(fd) {
		os.Stdout.WriteString("NOTFG\n")
		os.Exit(2)
	}
	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		os.Stdout.WriteString("STARTFAIL\n")
		os.Exit(3)
	}
	defer func() { _ = child.Process.Kill() }()
	if !SetForeground(fd, child.Process.Pid) {
		os.Stdout.WriteString("SETFAIL\n")
		os.Exit(4)
	}
	if pgrp, ok := ForegroundPgrp(fd); !ok || pgrp != child.Process.Pid {
		os.Stdout.WriteString("GETFAIL\n")
		os.Exit(5)
	}
	if IsForeground(fd) {
		os.Stdout.WriteString("STILLFG\n")
		os.Exit(6)
	}
	if !SetForeground(fd, OwnPgrp()) {
		os.Stdout.WriteString("RESTOREFAIL\n")
		os.Exit(7)
	}
	if !IsForeground(fd) {
		os.Stdout.WriteString("NORESTORE\n")
		os.Exit(8)
	}
	os.Stdout.WriteString("OK\n")
	os.Exit(0)
}

func openTestPTY(t *testing.T) (*os.File, *os.File, error) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	ptn, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(ptn), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

func TestTermiosRoundtrip(t *testing.T) {
	master, slave, err := openTestPTY(t)
	if err != nil {
		t.Skipf("分配 pty: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	fd := int(slave.Fd())
	saved, err := GetTermios(fd)
	if err != nil {
		t.Fatalf("GetTermios: %v", err)
	}
	raw := saved
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Oflag &^= unix.OPOST
	if err := SetTermios(fd, raw); err != nil {
		t.Fatalf("SetTermios: %v", err)
	}
	got, err := GetTermios(fd)
	if err != nil {
		t.Fatalf("GetTermios: %v", err)
	}
	if got.Lflag&unix.ICANON != 0 || got.Lflag&unix.ECHO != 0 || got.Oflag&unix.OPOST != 0 {
		t.Fatalf("raw 未生效: %+v", got)
	}
	if err := SetTermiosFlush(fd, saved); err != nil {
		t.Fatalf("SetTermiosFlush: %v", err)
	}
	back, err := GetTermios(fd)
	if err != nil {
		t.Fatalf("GetTermios: %v", err)
	}
	if back != saved {
		t.Fatalf("未还原:\n saved=%+v\n back =%+v", saved, back)
	}
}

func TestTermiosInvalidFd(t *testing.T) {
	if _, err := GetTermios(-1); err == nil {
		t.Error("非法 fd 应报错")
	}
	if err := SetTermios(-1, Termios{}); err == nil {
		t.Error("非法 fd 应报错")
	}
}

func TestResetModesWritesEscapeState(t *testing.T) {
	master, slave, err := openTestPTY(t)
	if err != nil {
		t.Skipf("分配 pty: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	if !ResetModes(slave) {
		t.Fatal("ResetModes 应写入成功")
	}
	buf := make([]byte, 128)
	n, err := master.Read(buf)
	if err != nil {
		t.Fatalf("读 pty: %v", err)
	}
	got := string(buf[:n])
	for _, want := range []string{"\x1b[0m", "\x1b[?25h", "\x1b[?7h", "\x1b[r", "\x1b[?1049l", "\x1b[?1000l", "\x1b[?1006l"} {
		if !strings.Contains(got, want) {
			t.Errorf("模式复位缺 %q: %q", want, got)
		}
	}
	if ResetModes(nil) {
		t.Error("nil tty 应返回 false")
	}
}
