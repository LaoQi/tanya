//go:build linux || darwin

package shell

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/LaoQi/tanya/ctty"
)

var posixCandidates = []string{"bash", "sh", "ash"}

var posixPrograms = []string{
	"ls", "cat", "head", "tail", "grep", "rg", "fd", "sed", "awk",
	"find", "sort", "wc", "cut", "tr", "xargs",
	"git", "curl", "wget", "go", "node", "python",
}

func posixCapabilities(*profile) string {
	return "管道与文本工具链（grep/sed/awk/xargs 等）可直接组合；交互式程序（sudo/ssh/gpg 等）的提示写入控制终端 /dev/tty，用户在终端可见并可直接应答；被信号终止的命令按 128+signum 记退出码。"
}

func posixConfigureGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func posixKillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return os.ErrProcessDone
	}
	return err
}

func posixProtectSignals() {
	ctty.ProtectJobSignals()
}

func posixExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), true
	}
	return exitErr.ExitCode(), true
}
