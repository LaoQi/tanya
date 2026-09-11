package repl

import (
	"strings"
	"testing"
)

func TestIsDirChangeCmd(t *testing.T) {
	for _, line := range []string{"cd", "cd -", "cd /tmp", "cd a b", `cd "a b"`, "pushd /tmp", "popd", "cd /tmp && ls", "cd; ls"} {
		if !isDirChangeCmd(line) {
			t.Errorf("%q 应判为目录命令", line)
		}
	}
	for _, line := range []string{"echo cd", `git commit -m "cd"`, "ls -d /tmp", "cdup /tmp"} {
		if isDirChangeCmd(line) {
			t.Errorf("%q 不应判为目录命令", line)
		}
	}
}

func TestRunShellLineBlocksDirChange(t *testing.T) {
	r := newTestREPL(t)
	for _, line := range []string{"cd", "cd -", "cd /tmp", `cd "a b"`, "cd a b", "cd /tmp && ls", "pushd /tmp", "popd"} {
		out := captureStdout(func() { r.runShellLine(line) })
		if !strings.Contains(out, MsgCdBlocked) {
			t.Errorf("%q 应拦截并提示: %q", line, out)
		}
		if strings.Contains(out, "ls:") {
			t.Errorf("%q 被拦截后复合命令不应执行: %q", line, out)
		}
	}
}
