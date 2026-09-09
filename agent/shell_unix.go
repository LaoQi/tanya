//go:build unix

package agent

import (
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
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
		signal.Ignore(syscall.SIGTTIN, syscall.SIGTTOU)
	})
}
