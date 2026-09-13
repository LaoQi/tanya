package agent

import (
	"io"
	"os"
	"os/exec"
)

type TTYBridge interface {
	Prepare(cmd *exec.Cmd) (*os.File, error)
	Attach(capture io.Writer) (stop func(), err error)
}
