//go:build !linux && !darwin && !windows

package readline

import (
	"io"
	"os/exec"
)

func lendStdinImpl() (Lease, error) { return nullLease{}, nil }

func lendFullImpl(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	return nil, ErrUnsupported
}
