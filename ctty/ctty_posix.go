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
