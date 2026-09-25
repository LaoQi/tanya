package shell

import (
	"io"
	"os"
	"os/exec"
)

type Bridge interface {
	Prepare(cmd *exec.Cmd) (*os.File, error)
	Attach(capture io.Writer) (stop func(), err error)
}
