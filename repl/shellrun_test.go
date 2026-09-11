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

func TestCdTarget(t *testing.T) {
	cases := []struct {
		line string
		want string
		ok   bool
	}{
		{"cd", "", true},
		{"cd -", "-", true},
		{"cd /tmp", "/tmp", true},
		{"cd a b", "", false},
		{"ls", "", false},
		{"cdx", "", false},
	}
	for _, c := range cases {
		got, ok := cdTarget(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("cdTarget(%q) = (%q, %v)，期望 (%q, %v)", c.line, got, ok, c.want, c.ok)
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

func TestShellCdLike(t *testing.T) {
	for _, line := range []string{"cd", "cd /tmp", "cd a b", `cd "a b"`, "pushd /tmp", "popd", "cd /tmp && ls", "cd; ls"} {
		if !shellCdLike(line) {
			t.Errorf("%q 应判为 cd 类命令", line)
		}
	}
	for _, line := range []string{"echo cd", `git commit -m "cd"`, "ls -d /tmp", "cdup /tmp"} {
		if shellCdLike(line) {
			t.Errorf("%q 不应判为 cd 类命令", line)
		}
	}
}

func TestRunShellLineCdLikeBlocked(t *testing.T) {
	r := newTestREPL(t)
	dir := t.TempDir()
	r.changeDir(dir)
	for _, line := range []string{`cd "a b"`, "cd a b", "cd /tmp && ls", "pushd /tmp"} {
		out := captureStdout(func() { r.runShellLine(line) })
		if !strings.Contains(out, MsgCdSubshell) {
			t.Errorf("%q 应给出黄色警告: %q", line, out)
		}
		if strings.Contains(out, "ls:") {
			t.Errorf("%q 被拦截后不应执行命令: %q", line, out)
		}
		if want, _ := filepath.EvalSymlinks(dir); r.cwd != want {
			t.Errorf("%q 不应改变 cwd: %q", line, r.cwd)
		}
	}
}

func newTestREPL(t *testing.T) *REPL {
	t.Helper()
	r, err := NewREPL(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestChangeDir(t *testing.T) {
	r := newTestREPL(t)
	dir := t.TempDir()
	before := r.baseCwd()
	r.changeDir(dir)
	if want, _ := filepath.EvalSymlinks(dir); r.cwd != want {
		t.Errorf("cwd = %q，期望 %q", r.cwd, want)
	}
	if r.prevCwd != before {
		t.Errorf("prevCwd = %q，期望 %q", r.prevCwd, before)
	}
	r.changeDir("-")
	if got, _ := filepath.EvalSymlinks(dir); r.prevCwd != got && r.cwd != before {
		t.Errorf("cd - 应折返: cwd=%q prev=%q", r.cwd, r.prevCwd)
	}
}

func TestChangeDirRelativeAndHome(t *testing.T) {
	r := newTestREPL(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r.changeDir(dir)
	r.changeDir("sub")
	if want, _ := filepath.EvalSymlinks(sub); r.cwd != want {
		t.Errorf("相对路径应基于当前 cwd: %q", r.cwd)
	}
	r.changeDir("~")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无 HOME")
	}
	if want, _ := filepath.EvalSymlinks(home); r.cwd != want {
		t.Errorf("cd ~ = %q，期望 %q", r.cwd, want)
	}
}

func TestChangeDirInvalid(t *testing.T) {
	r := newTestREPL(t)
	dir := t.TempDir()
	r.changeDir(dir)
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{filepath.Join(dir, "missing"), file} {
		out := captureStdout(func() { r.changeDir(arg) })
		if !strings.Contains(out, "不是目录") {
			t.Errorf("%q 应报不是目录: %q", arg, out)
		}
		if want, _ := filepath.EvalSymlinks(dir); r.cwd != want {
			t.Errorf("%q 失败后 cwd 不应变化: %q", arg, r.cwd)
		}
	}
	out := captureStdout(func() { r.changeDir("-") })
	if strings.Contains(out, "不是目录") {
		t.Errorf("有上一目录时 cd - 不应报错: %q", out)
	}
	r.prevCwd = ""
	out = captureStdout(func() { r.changeDir("-") })
	if !strings.Contains(out, "没有上一个目录") {
		t.Errorf("无上一目录时应报错: %q", out)
	}
}

func TestRunShellLineCd(t *testing.T) {
	r := newTestREPL(t)
	dir := t.TempDir()
	captureStdout(func() { r.runShellLine("cd " + dir) })
	if want, _ := filepath.EvalSymlinks(dir); r.cwd != want {
		t.Errorf("runShellLine 应内建 cd: %q", r.cwd)
	}
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
