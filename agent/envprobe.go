package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type envProbeData struct {
	bashPath  string
	workspace string
}

type envProbeFunc func(cwd string) envProbeData

var bashOnce sync.Once
var bashPathCache string

func defaultEnvProbe(cwd string) envProbeData {
	var d envProbeData
	bashOnce.Do(func() {
		if p, err := exec.LookPath("bash"); err == nil {
			bashPathCache = p
		}
	})
	d.bashPath = bashPathCache
	d.workspace = workspaceMarker(cwd)
	return d
}

func envSection(cwd string, probe envProbeFunc) string {
	data := probe(cwd)
	var b strings.Builder
	b.WriteString("# 环境\n")
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "CWD: %s\n", shortPath(cwd))
	shell := shellCommand
	if data.bashPath != "" {
		shell = data.bashPath
	}
	fmt.Fprintf(&b, "SHELL: %s %s（非交互，无 TTY）\n", shell, shellArg)
	fmt.Fprintf(&b, "TIMEOUT: 默认 %ds，上限 %ds\n", shellTimeoutSec, shellTimeoutLimit)
	fmt.Fprintf(&b, "OUTPUT: stdout/stderr 头尾各 %dKB，中间截断\n", shellMaxOutput/1000)
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
