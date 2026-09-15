//go:build linux || darwin

package ctty

import (
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

const Supported = true

func Open() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

func OwnPgrp() int {
	pgrp, err := unix.Getpgid(0)
	if err != nil {
		return -1
	}
	return pgrp
}

func ForegroundPgrp(fd int) (int, bool) {
	pgrp, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil {
		return 0, false
	}
	return pgrp, true
}

func SetForeground(fd, pgrp int) bool {
	return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, pgrp) == nil
}

func IsForeground(fd int) bool {
	pgrp, ok := ForegroundPgrp(fd)
	return ok && pgrp == OwnPgrp()
}

func IgnoreJobSignals() {
	signal.Ignore(unix.SIGTTIN, unix.SIGTTOU)
}

const resetModes = "\x1b[0m\x1b[?25h\x1b[?7h\x1b[r\x1b[?1049l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l"

func ResetModes(tty *os.File) bool {
	if tty == nil {
		return false
	}
	_, err := tty.WriteString(resetModes)
	return err == nil
}
