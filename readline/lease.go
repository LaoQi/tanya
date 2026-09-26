package readline

import (
	"io"
	"os"
	"os/exec"
)

type Lease interface {
	Stdin() *os.File
	Release()
}

type stdinLease struct {
	f       *os.File
	release func()
}

func (l *stdinLease) Stdin() *os.File { return l.f }

func (l *stdinLease) Release() {
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
	if l.release != nil {
		r := l.release
		l.release = nil
		r()
	}
}

func newStdinLease() (*stdinLease, error) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		return nil, err
	}
	return &stdinLease{f: f, release: anchorTerminal()}, nil
}

func (c *consoleImpl) LendStdin() (Lease, error) {
	return newStdinLease()
}

func (c *consoleImpl) LendFull(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	return lendFullImpl(cmd, capture)
}
