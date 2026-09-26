package shell

import (
	"io"
	"os"
	"os/exec"
)

type Lease interface {
	Stdin() *os.File
	Release()
}

type Console interface {
	LendStdin() (Lease, error)
	LendFull(cmd *exec.Cmd, capture io.Writer) (Lease, error)
}

type nullLease struct{}

func (nullLease) Stdin() *os.File { return nil }
func (nullLease) Release()        {}

type nullConsole struct{}

func (nullConsole) LendStdin() (Lease, error) { return nullLease{}, nil }

func (nullConsole) LendFull(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	return nil, ErrNoLend
}
