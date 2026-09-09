//go:build !unix

package agent

import (
	"os"
	"os/exec"
)

func configureProcessGroup(cmd *exec.Cmd) {}

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
