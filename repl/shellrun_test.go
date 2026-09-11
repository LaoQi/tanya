package repl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LaoQi/tanyan/agent"
)

func TestDialogueText(t *testing.T) {
	cases := []struct {
		line string
		want string
		ok   bool
	}{
		{":你好", "你好", true},
		{": 你好", "你好", true},
		{"：你好", "你好", true},
		{":", "", true},
		{"：  ", "", true},
		{"::x", ":x", true},
		{"/help", "", false},
		{"ls", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := dialogueText(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("dialogueText(%q) = (%q, %v)，期望 (%q, %v)", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestIsSlashCommand(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"/help", true},
		{"/help 参数", true},
		{"/quit", true},
		{"/usr/bin/ls", false},
		{"/tmp/a b", false},
		{"/unknown", false},
		{"ls", false},
		{"/", false},
	}
	for _, c := range cases {
		if got := isSlashCommand(c.line); got != c.want {
			t.Errorf("isSlashCommand(%q) = %v，期望 %v", c.line, got, c.want)
		}
	}
}

func TestIsExitLine(t *testing.T) {
	for _, line := range []string{"exit", "quit", "exit 1"} {
		if !isExitLine(line) {
			t.Errorf("%q 应内建退出", line)
		}
	}
	for _, line := range []string{"/exit", "exitfoo", "sudo exit", ""} {
		if isExitLine(line) {
			t.Errorf("%q 不应内建退出", line)
		}
	}
}

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	b, _ := io.ReadAll(r)
	return string(b)
}

func newTestREPL(t *testing.T) *REPL {
	t.Helper()
	r, err := NewREPL(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRunShellLineExecutes(t *testing.T) {
	r := newTestREPL(t)
	dir := t.TempDir()
	r.changeDir(dir)
	out := captureStdout(func() { r.runShellLine("echo hi") })
	if !strings.Contains(out, "hi\n") {
		t.Errorf("命令输出应直通 stdout: %q", out)
	}
	out = captureStdout(func() { r.runShellLine("pwd") })
	want, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(out, want) {
		t.Errorf("命令应在 REPL cwd 执行: %q，期望含 %q", out, want)
	}
	out = captureStderr(func() { r.runShellLine("exists-nowhere-xyz") })
	if !strings.Contains(out, "退出码 127") {
		t.Errorf("非零退出码应提示到 stderr: %q", out)
	}
}

func TestReportShellExit(t *testing.T) {
	cmd := agent.NewShellCmd("exit 2")
	err := cmd.Run()
	out := captureStderr(func() { reportShellExit(err, false) })
	if !strings.Contains(out, fmt.Sprintf(MsgShellExitCode, 2)) {
		t.Errorf("应提示退出码 2: %q", out)
	}
	out = captureStdout(func() { reportShellExit(nil, false) })
	if out != "" {
		t.Errorf("成功不应输出: %q", out)
	}
	out = captureStdout(func() { reportShellExit(err, true) })
	if !strings.Contains(out, MsgShellSuspended) {
		t.Errorf("挂起应提示: %q", out)
	}
}
