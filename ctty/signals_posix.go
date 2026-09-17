//go:build linux || darwin

package ctty

import (
	"os"

	"golang.org/x/sys/unix"
)

var exitSignals = []os.Signal{unix.SIGTERM, unix.SIGHUP}

var interruptSignals = []os.Signal{unix.SIGINT}

func emergencyRestore() {
	tty, err := Open()
	if err != nil {
		return
	}
	defer tty.Close()
	fd := int(tty.Fd())
	if !IsForeground(fd) {
		return
	}
	if t, err := GetTermios(fd); err == nil {
		sane := t
		sane.Iflag |= unix.ICRNL | unix.IXON
		sane.Lflag |= unix.ISIG | unix.ICANON | unix.ECHO | unix.IEXTEN
		sane.Oflag |= unix.OPOST | unix.ONLCR
		if sane != t {
			_ = SetTermios(fd, sane)
		}
	}
	_ = ResetModes(tty)
}
