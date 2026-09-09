//go:build unix && !illumos && !ios

package agent

import (
	"os"

	"golang.org/x/sys/unix"
)

func ownPgrp() int {
	pgrp, err := unix.Getpgid(0)
	if err != nil {
		return -1
	}
	return pgrp
}

func openForegroundTTY() *os.File {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	return tty
}

func handoverForeground(tty *os.File, pid int) bool {
	if tty == nil {
		return false
	}
	cur, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCGPGRP)
	if err != nil || cur != ownPgrp() {
		return false
	}
	return unix.IoctlSetPointerInt(int(tty.Fd()), unix.TIOCSPGRP, pid) == nil
}

func restoreForeground(tty *os.File, handed bool) {
	if tty == nil {
		return
	}
	defer tty.Close()
	if !handed {
		return
	}
	_ = unix.IoctlSetPointerInt(int(tty.Fd()), unix.TIOCSPGRP, ownPgrp())
}
