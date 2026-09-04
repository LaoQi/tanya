package agent

import (
	"strings"
	"testing"
)

func TestRunShellStdout(t *testing.T) {
	got := RunShell("echo hello", 10)
	if !strings.Contains(got, "stdout:\nhello") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellExitCode(t *testing.T) {
	got := RunShell("exit 3", 10)
	if !strings.Contains(got, "exit code: 3") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellStderr(t *testing.T) {
	got := RunShell("echo oops >&2", 10)
	if !strings.Contains(got, "stderr:\noops") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellNoOutput(t *testing.T) {
	got := RunShell("true", 10)
	if !strings.Contains(got, "退出码 0") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellTimeout(t *testing.T) {
	got := RunShell("sleep 5", 1)
	if !strings.Contains(got, "超时") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellTimeoutClamp(t *testing.T) {
	got := RunShell("echo ok", 9999)
	if !strings.Contains(got, "ok") {
		t.Errorf("got %q", got)
	}
}

func TestRunShellTruncation(t *testing.T) {
	got := RunShell("head -c 40000 /dev/zero | tr '\\0' 'a'", 30)
	if !strings.Contains(got, "截断") {
		t.Errorf("应包含截断标记: len=%d", len(got))
	}
	if len(got) > shellMaxOutput+200 {
		t.Errorf("截断后仍过长: %d", len(got))
	}
}

func TestTruncateOutput(t *testing.T) {
	s := strings.Repeat("a", 100)
	if got := truncateOutput(s, 200); got != s {
		t.Error("未超限不应截断")
	}
	got := truncateOutput(strings.Repeat("a", 100), 50)
	if !strings.Contains(got, "截断 50 字节") {
		t.Errorf("got %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 40)) {
		t.Error("应保留头部 80%")
	}
	if !strings.HasSuffix(got, strings.Repeat("a", 10)) {
		t.Error("应保留尾部 20%")
	}
}
