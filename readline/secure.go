//go:build linux || darwin

package readline

import (
	"github.com/LaoQi/tanya/ctty"
	"golang.org/x/sys/unix"
)

var terminalGuardOwns bool

func InitTerminalGuard() {
	ctty.IgnoreJobSignals()
	tty, err := ctty.Open()
	if err != nil {
		return
	}
	defer tty.Close()
	pgrp, ok := ctty.ForegroundPgrp(int(tty.Fd()))
	terminalGuardOwns = ok && pgrp == ctty.OwnPgrp()
}

func SecureTerminal() {
	if !terminalGuardOwns {
		return
	}
	tty, err := ctty.Open()
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
	if t, err := ctty.GetTermios(fd); err == nil {
		sane := t
		sane.Iflag |= unix.ICRNL | unix.IXON
		sane.Lflag |= unix.ISIG | unix.ICANON | unix.ECHO | unix.IEXTEN
		sane.Oflag |= unix.OPOST | unix.ONLCR
		if sane != t {
			_ = ctty.SetTermios(fd, sane)
		}
	}
	if pgrp, ok := ctty.ForegroundPgrp(fd); ok && pgrp != ctty.OwnPgrp() {
		ctty.SetForeground(fd, ctty.OwnPgrp())
	}
}
