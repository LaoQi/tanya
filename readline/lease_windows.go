//go:build windows

package readline

import (
	"io"
	"os"
	"os/exec"

	"github.com/LaoQi/tanya/ctty"
)

type consoleLease struct {
	tty    *os.File
	masked bool
}

func (l *consoleLease) Stdin() *os.File { return l.tty }

func (l *consoleLease) Release() {
	if l.masked {
		ctty.RestoreCtrlEvents()
		l.masked = false
	}
	if l.tty != nil {
		l.tty.Close()
		l.tty = nil
	}
}

func lendFullImpl(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	tty, err := ctty.Open()
	if err != nil || tty == nil {
		return nil, ErrUnsupported
	}
	cmd.Stdin = tty
	ctty.IgnoreCtrlEvents()
	return &consoleLease{tty: tty, masked: true}, nil
}
