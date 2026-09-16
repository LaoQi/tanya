//go:build linux || darwin

package agent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/LaoQi/tanya/ctty"
)

var platform = shellPlatform{
	Candidates:     []string{"bash", "sh", "ash"},
	ConfigureGroup: posixConfigureGroup,
	KillGroup:      posixKillGroup,
	ProtectSignals: posixProtectSignals,
	ExitCode:       posixExitCode,
	ProcessStopped: posixProcessStopped,
	Programs: []string{
		"ls", "cat", "head", "tail", "grep", "rg", "fd", "sed", "awk",
		"find", "sort", "wc", "cut", "tr", "xargs",
		"git", "curl", "wget", "go", "node", "python",
	},
	Capabilities: posixCapabilities,
}

func posixCapabilities(*shellProfile) string {
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
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGTSTP)
	ctty.IgnoreJobSignals()
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

func posixProcessStopped(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	return statState(string(b)) == "T"
}

func statState(stat string) string {
	i := strings.IndexByte(stat, ')')
	if i < 0 || i+2 > len(stat) {
		return ""
	}
	fields := strings.Fields(stat[i+2:])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
