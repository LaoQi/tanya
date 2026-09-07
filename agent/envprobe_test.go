package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeProbe(workspace string) envProbeFunc {
	return func(string) envProbeData {
		return envProbeData{workspace: workspace}
	}
}

func TestEnvSectionFull(t *testing.T) {
	withShellRuntime(t, &shellRuntime{profile: &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix}})
	out := envSection("/home/u/proj", fakeProbe("go.mod, Makefile"))
	wantSub := []string{
		"# 环境",
		"OS: " + runtime.GOOS + "/" + runtime.GOARCH,
		"SHELL: /usr/bin/bash -c（非交互，无 TTY）",
		"TIMEOUT: 默认 60s，上限 900s",
		"OUTPUT: stdout/stderr 头尾各 30KB，中间截断",
		"WORKSPACE: go.mod, Makefile",
	}
	for _, w := range wantSub {
		if !strings.Contains(out, w) {
			t.Errorf("envSection 缺 %q: %q", w, out)
		}
	}
}

func TestEnvSectionNoShell(t *testing.T) {
	withShellRuntime(t, &shellRuntime{})
	out := envSection("/tmp/x", fakeProbe(""))
	for _, line := range []string{"SHELL:", "TIMEOUT:", "OUTPUT:"} {
		if strings.Contains(out, line) {
			t.Errorf("无 shell 不应输出 %s 行: %q", line, out)
		}
	}
	if strings.Contains(out, "WORKSPACE:") {
		t.Errorf("无标记应省略 WORKSPACE 行: %q", out)
	}
}

func TestEnvSectionDeterministic(t *testing.T) {
	withShellRuntime(t, &shellRuntime{profile: &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix}})
	cwd := "/home/u/proj"
	probe := fakeProbe("go.mod")
	if envSection(cwd, probe) != envSection(cwd, probe) {
		t.Error("同参数两次渲染应字节相同")
	}
}

func TestWorkspaceMarker(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"go.mod", "package.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := workspaceMarker(dir); got != "go.mod, package.json" {
		t.Errorf("workspaceMarker = %q", got)
	}
	if got := workspaceMarker(t.TempDir()); got != "" {
		t.Errorf("空目录应为空串: %q", got)
	}
}

func TestShortPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无 HOME")
	}
	if got := shortPath(home); got != "~" {
		t.Errorf("shortPath(home) = %q", got)
	}
	sub := filepath.Join(home, "x")
	if got := shortPath(sub); got != "~/x" {
		t.Errorf("shortPath(sub) = %q", got)
	}
	if got := shortPath("/usr/share"); got != "/usr/share" {
		t.Errorf("外部路径应原样: %q", got)
	}
}
