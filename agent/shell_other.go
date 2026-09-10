//go:build !unix

package agent

import (
	"errors"
	"os"
	"os/exec"
)

func configureProcessGroup(cmd *exec.Cmd) {}

func ttyStdinSupported() bool { return false }

func openForegroundTTY() *os.File { return nil }

func handoverForeground(tty *os.File, pid int) bool { return false }

func restoreForeground(tty *os.File, handed bool) {}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}

func ProtectTerminalSignals() {}

func shellExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	return exitErr.ExitCode(), true
}
