//go:build unix

package agent

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/LaoQi/tanya/ctty"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return os.ErrProcessDone
	}
	return err
}

var protectOnce sync.Once

func ProtectTerminalSignals() {
	protectOnce.Do(func() {
		ch := make(chan os.Signal, 4)
		signal.Notify(ch, syscall.SIGTSTP)
		ctty.IgnoreJobSignals()
	})
}

func shellExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), true
	}
	return exitErr.ExitCode(), true
}
