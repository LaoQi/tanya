package shell

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"sync"
)

type shellPlatform struct {
	GOOS           string
	Candidates     []string
	ConfigureGroup func(*exec.Cmd)
	KillGroup      func(*exec.Cmd) error
	ProtectSignals func()
	ExitCode       func(error) (int, bool)
	ProcessStopped func(int) bool
	Programs       []string
	Capabilities   func(*profile) string
	DecodeOutput   func([]byte) string
}

var protectOnce sync.Once

func ProtectTerminalSignals() { protectOnce.Do(platform.ProtectSignals) }

func fillDefaults(p shellPlatform) shellPlatform {
	if p.GOOS == "" {
		p.GOOS = runtime.GOOS
	}
	if p.ConfigureGroup == nil {
		p.ConfigureGroup = noopConfigureGroup
	}
	if p.KillGroup == nil {
		p.KillGroup = defaultKillGroup
	}
	if p.ProtectSignals == nil {
		p.ProtectSignals = noopProtectSignals
	}
	if p.ExitCode == nil {
		p.ExitCode = defaultExitCode
	}
	if p.ProcessStopped == nil {
		p.ProcessStopped = neverStopped
	}
	if p.Capabilities == nil {
		p.Capabilities = noopCapabilities
	}
	if p.DecodeOutput == nil {
		p.DecodeOutput = identityOutput
	}
	return p
}

func identityOutput(b []byte) string { return string(b) }

func noopConfigureGroup(*exec.Cmd) {}

func noopProtectSignals() {}

func neverStopped(int) bool { return false }

func noopCapabilities(*profile) string { return "" }

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
