package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
func TestCdLikeLine(t *testing.T) {
	for _, line := range []string{"cd", "cd /tmp", "cd a b", `cd "a b"`, "pushd /tmp", "popd", "cd /tmp && ls", "cd; ls"} {
		if !cdLikeLine(line) {
			t.Errorf("%q 应判为 cd 类命令", line)
		}
	}
	for _, line := range []string{"echo cd", `git commit -m "cd"`, "ls -d /tmp", "cdup /tmp"} {
		if cdLikeLine(line) {
			t.Errorf("%q 不应判为 cd 类命令", line)
		}
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
