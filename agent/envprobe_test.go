package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeProbe(bashPath, workspace string) envProbeFunc {
	return func(string) envProbeData {
		return envProbeData{bashPath: bashPath, workspace: workspace}
	}
}

func TestEnvSectionFull(t *testing.T) {
	out := envSection("/home/u/proj", fakeProbe("/usr/bin/bash", "go.mod, Makefile"))
	wantSub := []string{
		"# 环境",
		"OS: " + runtime.GOOS + "/" + runtime.GOARCH,
		"SHELL: /usr/bin/bash -c（非交互，无 TTY）",
		"TIMEOUT: 默认 60s，上限 300s",
		"OUTPUT: stdout/stderr 头尾各 30KB，中间截断",
		"WORKSPACE: go.mod, Makefile",
	}
	for _, w := range wantSub {
		if !strings.Contains(out, w) {
			t.Errorf("envSection 缺 %q: %q", w, out)
		}
	}
}

func TestEnvSectionFallbacks(t *testing.T) {
	out := envSection("/tmp/x", fakeProbe("", ""))
	if !strings.Contains(out, "SHELL: bash -c（非交互，无 TTY）") {
		t.Errorf("无 bash 路径应回退 bash: %q", out)
	}
	if strings.Contains(out, "WORKSPACE:") {
		t.Errorf("无标记应省略 WORKSPACE 行: %q", out)
	}
}

func TestEnvSectionDeterministic(t *testing.T) {
	cwd := "/home/u/proj"
	probe := fakeProbe("/usr/bin/bash", "go.mod")
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

func TestDefaultEnvProbeBashCached(t *testing.T) {
	d1 := defaultEnvProbe(".")
	d2 := defaultEnvProbe(".")
	if d1.bashPath != d2.bashPath {
		t.Error("bash 路径应包级缓存一致")
	}
}
