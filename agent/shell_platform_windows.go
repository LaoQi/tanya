//go:build windows

package agent

import (
	"os"
	"os/exec"
	"strconv"

	"github.com/LaoQi/tanya/ctty"
)

var platform = fillDefaults(shellPlatform{
	Candidates: []string{"pwsh", "powershell"},
	KillGroup:  windowsKillGroup,
	Programs: []string{
		"ls", "cat", "head", "tail", "grep", "sed", "awk", "wc", "cut", "tr", "xargs", "diff", "tee", "uniq",
		"rg", "fd", "git", "curl", "wget", "tar", "ssh", "go", "node", "python",
		"where", "findstr",
	},
	Capabilities: windowsCapabilities,
	DecodeOutput: windowsDecodeOutput,
})

func windowsDecodeOutput(b []byte) string {
	cp := ctty.FallbackCP()
	if cp == 0 {
		return string(b)
	}
	return string(ctty.DecodeCP(cp, b))
}

func windowsCapabilities(p *shellProfile) string {
	if p.Kind == KindPowerShell {
		return `命令在 PowerShell 中执行，管道传递对象而非纯文本，路径分隔符为 \，检索与文本处理优先用 PowerShell cmdlet（Get-ChildItem/Select-String/Where-Object 等）；交互式程序（interactive: true）的 stdin 继承控制台、可在终端直接应答，写控制台的提示（ssh 等）实时可见、写 stdout 的提示随输出捕获，无 /dev/tty 概念；unix 工具链（grep/sed/awk 等）需 coreutils、Git for Windows 或 msys2 提供，以「可用程序」清单为准。`
	}
	return `命令在 cmd.exe 中执行，路径分隔符为 \，文本处理优先用内建命令（dir/findstr/type 等）或 PowerShell 单行命令；交互式程序（interactive: true）的 stdin 继承控制台、可在终端直接应答，写控制台的提示（ssh 等）实时可见、写 stdout 的提示随输出捕获，无 /dev/tty 概念；unix 工具链（grep/sed/awk 等）需 coreutils、Git for Windows 或 msys2 提供，以「可用程序」清单为准。`
}

func windowsKillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	if err := exec.Command("taskkill", windowsKillArgs(cmd.Process.Pid)...).Run(); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func windowsKillArgs(pid int) []string {
	return []string{"/T", "/F", "/PID", strconv.Itoa(pid)}
}
