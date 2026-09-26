//go:build linux || darwin

package shell

import (
	"strings"
	"testing"
)

func TestPlatformPosixCandidates(t *testing.T) {
	if got := strings.Join(platform.Candidates, "/"); got != "bash/sh/ash" {
		t.Errorf("posix 候选链 = %q", got)
	}
}

func TestPlatformPosixPrograms(t *testing.T) {
	want := []string{
		"ls", "cat", "head", "tail", "grep", "rg", "fd", "sed", "awk",
		"find", "sort", "wc", "cut", "tr", "xargs",
		"git", "curl", "wget", "go", "node", "python",
	}
	if strings.Join(platform.Programs, ",") != strings.Join(want, ",") {
		t.Errorf("posix 程序清单 = %v", platform.Programs)
	}
}

func TestPlatformPosixCapabilities(t *testing.T) {
	want := "管道与文本工具链（grep/sed/awk/xargs 等）可直接组合；交互式程序（sudo/ssh/gpg/read 等）必须显式 interactive: true（命令与终端直通、可直接应答），普通命令的 stdin 不接终端；被信号终止的命令按 128+signum 记退出码。"
	if got := platform.Capabilities(&profile{Name: "bash", Kind: KindPosix}); got != want {
		t.Errorf("posix 能力描述不匹配:\n got %q\nwant %q", got, want)
	}
}
