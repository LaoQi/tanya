//go:build windows

package agent

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPlatformWindowsCandidates(t *testing.T) {
	if got := strings.Join(platform.Candidates, "/"); got != "pwsh/powershell" {
		t.Errorf("windows 候选链 = %q", got)
	}
}

func TestPlatformWindowsPrograms(t *testing.T) {
	want := []string{
		"ls", "cat", "head", "tail", "grep", "sed", "awk", "wc", "cut", "tr", "xargs", "diff", "tee", "uniq",
		"rg", "fd", "git", "curl", "wget", "tar", "ssh", "go", "node", "python",
		"where", "findstr",
	}
	if strings.Join(platform.Programs, ",") != strings.Join(want, ",") {
		t.Errorf("windows 程序清单 = %v", platform.Programs)
	}
}

func TestPlatformWindowsCapabilities(t *testing.T) {
	ps := platform.Capabilities(&shellProfile{Name: "pwsh", Kind: KindPowerShell})
	if !strings.Contains(ps, "管道传递对象而非纯文本") || !strings.Contains(ps, "cmdlet") || !strings.Contains(ps, "无 /dev/tty 概念") || !strings.Contains(ps, "coreutils") {
		t.Errorf("PowerShell 能力描述缺项: %q", ps)
	}
	cmd := platform.Capabilities(&shellProfile{Name: "cmd", Kind: KindCmd})
	if !strings.Contains(cmd, "cmd.exe") || !strings.Contains(cmd, "findstr") || strings.Contains(cmd, "cmdlet") {
		t.Errorf("cmd 能力描述缺项: %q", cmd)
	}
}

func TestPlatformWindowsDescGolden(t *testing.T) {
	cwdLine := "默认在会话启动目录（进程 cwd）下执行，无需 cd 进入项目；需要其它目录时用 cwd 参数，不必写 cd 前缀。"
	useLine := "读文件、搜索、文本处理等系统操作都用它。可用程序: "
	ps := "命令在 PowerShell 中执行，管道传递对象而非纯文本，路径分隔符为 \\，检索与文本处理优先用 PowerShell cmdlet（Get-ChildItem/Select-String/Where-Object 等）；交互式程序的提示直接在终端可见，无 /dev/tty 概念；unix 工具链（grep/sed/awk 等）需 coreutils、Git for Windows 或 msys2 提供，以「可用程序」清单为准。"
	cmd := "命令在 cmd.exe 中执行，路径分隔符为 \\，文本处理优先用内建命令（dir/findstr/type 等）或 PowerShell 单行命令；交互式程序的提示直接在终端可见，无 /dev/tty 概念；unix 工具链（grep/sed/awk 等）需 coreutils、Git for Windows 或 msys2 提供，以「可用程序」清单为准。"

	got := describeShell(platform, &shellProfile{Name: "pwsh", Kind: KindPowerShell}, []string{"git", "where"})
	want := "在 windows pwsh 中执行命令（PowerShell 语法），返回 stdout/stderr/退出码。" + cwdLine + ps + useLine + "git, where"
	if got != want {
		t.Errorf("windows PowerShell 描述不匹配:\n got %q\nwant %q", got, want)
	}
	got = describeShell(platform, &shellProfile{Name: "cmd", Kind: KindCmd}, nil)
	want = "在 windows cmd 中执行命令（cmd 语法），返回 stdout/stderr/退出码。" + cwdLine + cmd + "读文件、搜索、文本处理等系统操作都用它。"
	if got != want {
		t.Errorf("windows cmd 描述不匹配:\n got %q\nwant %q", got, want)
	}
}

func TestWindowsKillArgs(t *testing.T) {
	if got := strings.Join(windowsKillArgs(4242), " "); got != "/T /F /PID 4242" {
		t.Errorf("taskkill 参数 = %q", got)
	}
}

func TestWindowsKillGroupNilProcess(t *testing.T) {
	if err := windowsKillGroup(&exec.Cmd{}); err != os.ErrProcessDone {
		t.Errorf("空进程应返回 ErrProcessDone: %v", err)
	}
}
