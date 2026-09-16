package agent

import (
	"errors"
	"os"
	"os/exec"
	"sync"
)

type shellPlatform struct {
	Candidates     []string
	ConfigureGroup func(*exec.Cmd)
	KillGroup      func(*exec.Cmd) error
	ProtectSignals func()
	ExitCode       func(error) (int, bool)
	ProcessStopped func(int) bool
	Programs       []string
	Capabilities   func(*shellProfile) string
}

var protectOnce sync.Once

func ProtectTerminalSignals() { protectOnce.Do(platform.ProtectSignals) }

func noopConfigureGroup(*exec.Cmd) {}

func noopProtectSignals() {}

func neverStopped(int) bool { return false }

func defaultKillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}

func defaultExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	return exitErr.ExitCode(), true
}
