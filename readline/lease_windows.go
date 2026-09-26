//go:build windows

package readline

import (
	"io"
	"os"
	"os/exec"

	"github.com/LaoQi/tanya/ctty"
)

type stdinLease struct {
	tty      *os.File
	saved    ctty.InputModes
	hasSaved bool
	masked   bool
}

func (l *stdinLease) Stdin() *os.File { return l.tty }

func (l *stdinLease) Handover(pid int) bool { return false }

func (l *stdinLease) Release() {
	if l.tty == nil {
		return
	}
	ctty.RestoreCtrlEvents()
	if l.hasSaved {
		ctty.RestoreInput(int(l.tty.Fd()), l.saved)
	}
	l.tty.Close()
}

func lendStdinImpl() (Lease, error) {
	tty, err := ctty.Open()
	if err != nil || tty == nil {
		return nullLease{}, nil
	}
	l := &stdinLease{tty: tty}
	if s, ok := ctty.SnapshotInput(int(tty.Fd())); ok {
		l.saved, l.hasSaved = s, true
	}
	ctty.IgnoreCtrlEvents()
	l.masked = true
	return l, nil
}

func lendFullImpl(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	return nil, ErrUnsupported
}
