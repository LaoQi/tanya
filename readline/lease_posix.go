//go:build linux || darwin

package readline

import (
	"os"

	"github.com/LaoQi/tanya/ctty"
)

var (
	lendOpenTTY  = ctty.Open
	lendIsForegr = ctty.IsForeground
)

type stdinLease struct {
	tty      *os.File
	saved    ctty.InputModes
	hasSaved bool
	anchored bool
	handed   bool
}

func (l *stdinLease) Stdin() *os.File { return l.tty }

func (l *stdinLease) Handover(pid int) bool {
	if l.tty == nil || !l.anchored {
		return false
	}
	l.handed = ctty.SetForeground(int(l.tty.Fd()), pid)
	return l.handed
}

func (l *stdinLease) Release() {
	if l.tty == nil {
		return
	}
	if l.hasSaved {
		ctty.RestoreInput(int(l.tty.Fd()), l.saved)
	}
	if l.handed {
		ctty.SetForeground(int(l.tty.Fd()), ctty.OwnPgrp())
	}
	if l.anchored && lendIsForegr(int(l.tty.Fd())) {
		ctty.ResetModes(l.tty)
		ctty.RestoreCursor(l.tty)
	}
	l.tty.Close()
	l.tty = nil
}

func lendStdinImpl() (Lease, error) {
	tty, err := lendOpenTTY()
	if err != nil || tty == nil {
		return nullLease{}, nil
	}
	l := &stdinLease{tty: tty}
	if s, ok := ctty.SnapshotInput(int(tty.Fd())); ok {
		l.saved, l.hasSaved = s, true
	}
	if lendIsForegr(int(tty.Fd())) {
		l.anchored = ctty.SaveCursor(tty)
	}
	return l, nil
}
