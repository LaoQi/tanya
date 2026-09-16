//go:build windows

package agent

var platform = shellPlatform{
	Candidates:     []string{"pwsh", "powershell"},
	ConfigureGroup: noopConfigureGroup,
	KillGroup:      defaultKillGroup,
	ProtectSignals: noopProtectSignals,
	ExitCode:       defaultExitCode,
	ProcessStopped: neverStopped,
	Programs: []string{
		"ls", "cat", "head", "tail", "grep", "sed", "awk", "wc", "cut", "tr", "xargs", "diff", "tee", "uniq",
		"rg", "fd", "git", "curl", "wget", "tar", "ssh", "go", "node", "python",
		"where", "findstr",
	},
	Capabilities: windowsCapabilities,
}

func windowsCapabilities(p *shellProfile) string {
	if p.Kind == KindPowerShell {
		return `命令在 PowerShell 中执行，管道传递对象而非纯文本，路径分隔符为 \，检索与文本处理优先用 PowerShell cmdlet（Get-ChildItem/Select-String/Where-Object 等）；交互式程序的提示直接在终端可见，无 /dev/tty 概念；unix 工具链（grep/sed/awk 等）需 coreutils、Git for Windows 或 msys2 提供，以「可用程序」清单为准。`
	}
	return `命令在 cmd.exe 中执行，路径分隔符为 \，文本处理优先用内建命令（dir/findstr/type 等）或 PowerShell 单行命令；交互式程序的提示直接在终端可见，无 /dev/tty 概念；unix 工具链（grep/sed/awk 等）需 coreutils、Git for Windows 或 msys2 提供，以「可用程序」清单为准。`
}
