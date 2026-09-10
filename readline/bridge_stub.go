//go:build !linux

package readline

import (
	"io"
	"os"
	"os/exec"
)

type unsupportedBridge struct{}

func NewTTYBridge() TTYBridge { return unsupportedBridge{} }

func (unsupportedBridge) Prepare(cmd *exec.Cmd) (*os.File, error) {
	return nil, ErrUnsupported
}

func (unsupportedBridge) Attach(capture io.Writer) (func(), error) {
	return nil, ErrUnsupported
}
