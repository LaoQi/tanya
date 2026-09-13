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

func TestEnvSectionGolden(t *testing.T) {
	profile := &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix}
	want := "# 环境\n" +
		"OS: " + runtime.GOOS + "/" + runtime.GOARCH + "\n" +
		"CWD: /home/u/proj\n" +
		"SHELL: /usr/bin/bash -c（非交互"
	if ttyStdinSupported() {
		want += "；有控制终端时 run_shell 子进程 stdin 直通 tty，可应答密码/确认"
	}
	want += "）\n"
	if ttyStdinSupported() {
		want += "TTY: 交互提示须写入 /dev/tty 才可见（stdout/stderr 被工具捕获）\n"
	}
	want += "TIMEOUT: 默认 60s（interactive 时 300s），上限 900s\n" +
		"OUTPUT: stdout/stderr 头尾各 30KB，中间截断\n" +
		"WORKSPACE: go.mod, Makefile\n"
	got := envSection("/home/u/proj", fakeProbe("go.mod, Makefile"), profile)
	if got != want {
		t.Errorf("envSection 全串不匹配:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(envSection("/home/u/proj", fakeProbe(""), profile), "WORKSPACE:") {
		t.Error("无工作区标记时不应输出 WORKSPACE 行")
	}
}

func TestEnvSectionDeterministic(t *testing.T) {
	profile := &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix}
	cwd := "/home/u/proj"
	probe := fakeProbe("go.mod")
	if envSection(cwd, probe, profile) != envSection(cwd, probe, profile) {
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
