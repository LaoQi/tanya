//go:build unix && !illumos && !ios

package readline

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

var terminalGuardOwns bool

func InitTerminalGuard() {
	signal.Ignore(syscall.SIGTTIN, syscall.SIGTTOU)
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer tty.Close()
	cur, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCGPGRP)
	terminalGuardOwns = err == nil && cur == unix.Getpgrp()
}

func SecureTerminal() {
	if !terminalGuardOwns {
		return
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer tty.Close()
	secureTerminalFd(int(tty.Fd()), true)
}

func secureTerminalFd(fd int, owns bool) {
	if !owns {
		return
	}
	if t, err := getTermios(fd); err == nil && t.Lflag&unix.ISIG == 0 {
		t.Lflag |= unix.ISIG
		_ = setTermios(fd, t)
	}
	if cur, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP); err == nil && cur != unix.Getpgrp() {
		_ = unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, unix.Getpgrp())
	}
}
