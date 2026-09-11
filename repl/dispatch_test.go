package repl

import (
	"io"
	"os"
	"testing"
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
