//go:build !linux && !windows

package readline

import (
	"io"
	"os/exec"
)

func lendFullImpl(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	return nil, ErrUnsupported
}
