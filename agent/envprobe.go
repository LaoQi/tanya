package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type envProbeData struct {
	workspace string
}

type envProbeFunc func(cwd string) envProbeData

func defaultEnvProbe(cwd string) envProbeData {
	return envProbeData{workspace: workspaceMarker(cwd)}
}

func envSection(cwd string, probe envProbeFunc) string {
	data := probe(cwd)
	var b strings.Builder
	b.WriteString("# 环境\n")
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "CWD: %s\n", shortPath(cwd))
	if rt := ShellRuntime(); rt.profile != nil {
		ttyNote, ttyLine := "", ""
		if ttyStdinSupported() {
			ttyNote = "；有控制终端时 run_shell 子进程 stdin 直通 tty，可应答密码/确认"
			ttyLine = "TTY: 交互提示须写入 /dev/tty 才可见（stdout/stderr 被工具捕获）\n"
		}
		fmt.Fprintf(&b, "SHELL: %s（非交互%s）\n", rt.profile.invocation(), ttyNote)
		b.WriteString(ttyLine)
		fmt.Fprintf(&b, "TIMEOUT: 默认 %ds（interactive 时 %ds），上限 %ds\n", shellTimeoutSec, shellInteractiveTimeoutSec, shellTimeoutLimit)
		fmt.Fprintf(&b, "OUTPUT: stdout/stderr 头尾各 %dKB，中间截断\n", shellMaxOutput/1000)
	}
	if data.workspace != "" {
		fmt.Fprintf(&b, "WORKSPACE: %s\n", data.workspace)
	}
	return b.String()
}

func workspaceMarker(cwd string) string {
	files := []string{"go.mod", "package.json", "pyproject.toml", "requirements.txt",
		"Cargo.toml", "pom.xml", "CMakeLists.txt", "Makefile"}
	var found []string
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(cwd, f)); err == nil {
			found = append(found, f)
		}
	}
	return strings.Join(found, ", ")
}

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + p[len(home):]
		}
	}
	return p
}
