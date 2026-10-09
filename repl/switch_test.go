package repl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
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
	oldID := a.SessionID()
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
	if r.started.IsZero() {
		t.Error("切换后应重置计时起点")
	}
	got := out.String()
	dir, ok := a.SessionDir()
	if !ok {
		t.Fatal("切换后应有会话目录")
	}
	wantSwitch := fmt.Sprintf(MsgSwitchDone, initPath(from), initPath(target)) + fmt.Sprintf(MsgSwitchDir, initPath(dir))
	if !strings.HasSuffix(got, wantSwitch) {
		t.Errorf("切换报告应为结尾: got %q want 后缀 %q", got, wantSwitch)
	}
	head := strings.TrimSuffix(got, wantSwitch)
	if !strings.HasPrefix(head, "会话 "+oldID) {
		t.Errorf("旧会话收尾块应在前并含旧 id %q: %q", oldID, head)
	}
	if !strings.Contains(head, MsgStatNoUsage) {
		t.Errorf("收尾块应含用量行: %q", head)
	}
	if strings.Contains(head, "会话文件") {
		t.Errorf("旧会话未落盘不应有文件行: %q", head)
	}
	if got := r.cwdLabel(); got != shortPath(target) {
		t.Errorf("提示符 cwd 应跟随工作区: got %q want %q", got, shortPath(target))
	}
}

func TestSwitchRetireKeepsOldFile(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	oldID := a.SessionID()
	st := a.Stats()
	if err := os.WriteFile(st.Session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	if exit := r.handleCommand("/switch " + target); exit {
		t.Error("/switch 不应退出")
	}
	if errb.String() != "" {
		t.Fatalf("切换不应报错: %q", errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "会话 "+oldID) {
		t.Errorf("应含旧会话 id: %q", got)
	}
	if !strings.Contains(got, "会话文件 "+homePath(st.Session)) {
		t.Errorf("应含旧会话落地位置: %q", got)
	}
	dir, ok := a.SessionDir()
	if !ok {
		t.Fatal("切换后应有会话目录")
	}
	if homePath(dir) == homePath(filepath.Dir(st.Session)) {
		t.Errorf("切换后会话目录应改变: %q", dir)
	}
	if !strings.Contains(got, fmt.Sprintf(MsgSwitchDir, initPath(dir))) {
		t.Errorf("报告应含新会话目录 %q: %q", dir, got)
	}
}

func TestSwitchRetireNoSave(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir(), agent.NoSave(true))
	from := a.Workspace()
	target := t.TempDir()
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	if exit := r.handleCommand("/switch " + target); exit {
		t.Error("/switch 不应退出")
	}
	if errb.String() != "" {
		t.Fatalf("切换不应报错: %q", errb.String())
	}
	got := out.String()
	if !strings.Contains(got, MsgFarewellNoFile) {
		t.Errorf("不落盘模式应报未写入: %q", got)
	}
	wantSwitch := fmt.Sprintf(MsgSwitchDone, initPath(from), initPath(target))
	if !strings.HasSuffix(got, wantSwitch) {
		t.Errorf("不落盘时应只有切换报告收尾: got %q want 后缀 %q", got, wantSwitch)
	}
	if strings.Contains(got, "新会话目录") {
		t.Errorf("不落盘模式不应有会话目录行: %q", got)
	}
}

func TestSwitchRetireArchivedReadOnly(t *testing.T) {
	dataDir := t.TempDir()
	a := newSessTestAgent(t, dataDir)
	sdir, ok := a.SessionDir()
	if !ok {
		t.Fatal("应有会话目录")
	}
	id := "20260101-100000"
	path := filepath.Join(sdir, id+".jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"system","content":"sys"}
{"role":"user","content":"hi"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ArchiveSessions(agent.ArchiveOptions{Exclude: a.SessionID(), Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadSession(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.ArchiveReadOnly(); !ok {
		t.Fatal("应处于归档只读态")
	}
	from := a.Workspace()
	target := t.TempDir()
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	if exit := r.handleCommand("/switch " + target); exit {
		t.Error("/switch 不应退出")
	}
	if errb.String() != "" {
		t.Fatalf("切换不应报错: %q", errb.String())
	}
	got := out.String()
	if strings.Contains(got, "会话 "+id) {
		t.Errorf("归档只读态收尾块不应有会话 id: %q", got)
	}
	if strings.Contains(got, "会话文件") {
		t.Errorf("归档只读态文件行应省略（frozen 非 disabled，不触发未写入）: %q", got)
	}
	if !strings.HasPrefix(got, "时长 ") {
		t.Errorf("会话行应退化为无 id 形态: %q", got)
	}
	dir, ok := a.SessionDir()
	if !ok {
		t.Fatal("切走后新 store 应可写")
	}
	wantSwitch := fmt.Sprintf(MsgSwitchDone, initPath(from), initPath(target)) + fmt.Sprintf(MsgSwitchDir, initPath(dir))
	if !strings.HasSuffix(got, wantSwitch) {
		t.Errorf("切换报告应为结尾: %q", got)
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
