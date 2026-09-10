package agent

import (
	"io"
	"os"
	"os/exec"
	"sync"
)

type TTYBridge interface {
	Prepare(cmd *exec.Cmd) (*os.File, error)
	Attach(capture io.Writer) (stop func(), err error)
}

var (
	ttyBridgeMu  sync.Mutex
	ttyBridgeCur TTYBridge
)

func InitTTYBridge(b TTYBridge) {
	ttyBridgeMu.Lock()
	defer ttyBridgeMu.Unlock()
	ttyBridgeCur = b
}

func currentTTYBridge() TTYBridge {
	ttyBridgeMu.Lock()
	defer ttyBridgeMu.Unlock()
	return ttyBridgeCur
}
