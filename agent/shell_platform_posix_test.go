//go:build linux || darwin

package agent

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
	want := "管道与文本工具链（grep/sed/awk/xargs 等）可直接组合；交互式程序（sudo/ssh/gpg 等）的提示写入控制终端 /dev/tty，用户在终端可见并可直接应答；被信号终止的命令按 128+signum 记退出码。"
	if got := platform.Capabilities(&shellProfile{Name: "bash", Kind: KindPosix}); got != want {
		t.Errorf("posix 能力描述不匹配:\n got %q\nwant %q", got, want)
	}
}

func TestStatState(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"12345 (bash) S 1 2 3 4 5 6 7", "S"},
		{"12345 (a b) T 1 2 3 4 5 6 7", "T"},
		{"12345 (bash) R", "R"},
		{"broken", ""},
		{"12345 ()", ""},
	}
	for _, c := range cases {
		if got := statState(c.in); got != c.want {
			t.Errorf("statState(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
