package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LaoQi/tanyan/ctty"
)

func TestEnvSectionGolden(t *testing.T) {
	dir := t.TempDir()
	profile := &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix}
	want := "# 环境\n" +
		"OS: " + runtime.GOOS + "/" + runtime.GOARCH + "\n" +
		"CWD: " + dir + "\n" +
		"SHELL: bash\n"
	if ctty.Supported {
		want += "TTY: 交互提示须写入 /dev/tty 才可见（stdout/stderr 被工具捕获）\n"
	}
	want += "TIMEOUT: 默认 60s（interactive 时 300s），上限 900s\n" +
		"OUTPUT: stdout/stderr 头尾各 30KB，中间截断\n"
	if got := envSection(dir, profile); got != want {
		t.Errorf("envSection 全串不匹配:\n got %q\nwant %q", got, want)
	}
}

func TestEnvSectionDeterministic(t *testing.T) {
	profile := &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix}
	cwd := t.TempDir()
	if envSection(cwd, profile) != envSection(cwd, profile) {
		t.Error("同参数两次渲染应字节相同")
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
