package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSwitchUsageWithoutArg(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	if exit := r.handleCommand("/switch"); exit {
		t.Error("/switch 无参不应退出")
	}
	got := out.String()
	if !strings.Contains(got, "用法: /switch") || !strings.Contains(got, initPath(a.Workspace())) {
		t.Errorf("无参应打印用法与当前工作区: %q", got)
	}
	if errb.String() != "" {
		t.Errorf("无参不应报错: %q", errb.String())
	}
	if a.Workspace() == "" {
		t.Error("无参不应清空工作区")
	}
}

func TestSwitchCommandSwitchesWorkspace(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	target := t.TempDir()
	from := a.Workspace()
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	if exit := r.handleCommand("/switch " + target); exit {
		t.Error("/switch 不应退出")
	}
	if errb.String() != "" {
		t.Fatalf("切换不应报错: %q", errb.String())
	}
	if a.Workspace() != target {
		t.Errorf("工作区: got %q want %q", a.Workspace(), target)
	}
	want := strings.Replace(MsgSwitchDone, "%s", initPath(from), 1)
	want = strings.Replace(want, "%s", initPath(target), 1)
	if got := out.String(); !strings.HasPrefix(got, want) {
		t.Errorf("报告应以切换结果为前缀: got %q want 前缀 %q", got, want)
	}
	if dir, ok := a.SessionDir(); ok && !strings.Contains(out.String(), initPath(dir)) {
		t.Errorf("报告应含会话目录 %q: %q", dir, out.String())
	}
	if got := r.cwdLabel(); got != shortPath(target) {
		t.Errorf("提示符 cwd 应跟随工作区: got %q want %q", got, shortPath(target))
	}
}

func TestSwitchCommandKeepsStateOnFailure(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	from := a.Workspace()
	oldID := a.SessionID()
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{file, filepath.Join(t.TempDir(), "missing"), from} {
		r.handleCommand("/switch " + target)
		if errb.String() == "" {
			t.Errorf("%q 应报错", target)
		}
		if out.String() != "" {
			t.Errorf("%q 不应有正常输出: %q", target, out.String())
		}
		if a.Workspace() != from || a.SessionID() != oldID {
			t.Fatalf("失败后状态应不变: ws=%q id=%q", a.Workspace(), a.SessionID())
		}
	}
}

func TestSwitchWithoutAgent(t *testing.T) {
	r, out, errb := newTestREPL(t, newFakeTerm())
	if exit := r.handleCommand("/switch /tmp"); exit {
		t.Error("/switch 不应退出")
	}
	if out.String() != "" || errb.String() != "" {
		t.Errorf("agent 缺位时应静默: out=%q err=%q", out.String(), errb.String())
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := r.cwdLabel(); got != shortPath(cwd) {
		t.Errorf("agent 缺位时 cwd 标签应回落进程 cwd: got %q want %q", got, shortPath(cwd))
	}
}
