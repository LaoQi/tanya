package readline

import (
	"io"
	"os"
	"os/exec"
)

type Lease interface {
	Stdin() *os.File
	Handover(pid int) bool
	Release()
}

type nullLease struct{}

func (nullLease) Stdin() *os.File       { return nil }
func (nullLease) Handover(pid int) bool { return false }
func (nullLease) Release()              {}

func (c *consoleImpl) LendStdin() (Lease, error) {
	return lendStdinImpl()
}

func (c *consoleImpl) LendFull(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	return lendFullImpl(cmd, capture)
}
