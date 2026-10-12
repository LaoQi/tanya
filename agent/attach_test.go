package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDirFollowsSessionBase(t *testing.T) {
	a := newTestAgent(t)
	dir, ok := a.RunDir()
	if !ok {
		t.Fatal("落盘模式应有 run 目录")
	}
	if dir != RunDirFor(a.cfg, a.Workspace()) {
		t.Fatalf("RunDir 与 RunDirFor 不一致: %q", dir)
	}
	if base := filepath.Base(dir); base != "run" {
		t.Fatalf("run 目录名异常: %q", dir)
	}
	if sdir, _ := a.SessionDir(); filepath.Dir(sdir) != filepath.Dir(dir) {
		t.Fatalf("run 应与 sessions 同 base: %q %q", dir, sdir)
	}
}

func TestRunDirNoSave(t *testing.T) {
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	a, err := New(cfg, NoSave(true), WithSystemPrompt(testBasePrompt))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.RunDir(); ok {
		t.Error("不落盘模式不应有 run 目录")
	}
}

func TestAdoptSessionContinuesSameFile(t *testing.T) {
	a := newTestAgent(t)
	a.history = []Message{{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"}}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	file := a.SessionFile()
	if file == "" {
		t.Fatal("来源会话应已落盘")
	}
	peerSystem := a.prompt.snapshot

	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = a.cfg.DataDir
	b, err := New(cfg, WithSystemPrompt("OTHER-BASE"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AdoptSession(file); err != nil {
		t.Fatalf("AdoptSession: %v", err)
	}
	if len(b.History()) != 2 || b.History()[0].Content != "q1" {
		t.Fatalf("历史未继承: %+v", b.History())
	}
	if b.prompt.snapshot != peerSystem {
		t.Error("应沿用来源会话的 system 首行快照")
	}
	if b.SessionFile() != file {
		t.Fatalf("应指向同一会话文件: %q", b.SessionFile())
	}
	if dir := filepath.Dir(file); dir != mustSessionDir(t, b) {
		t.Fatalf("store 目录未随之切换: %q", dir)
	}
	b.history = append(b.history, Message{Role: "user", Content: "q2"})
	if err := b.save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("应续写为 4 行（system + 3 条）: %d\n%s", len(lines), data)
	}
	if !strings.Contains(lines[0], peerSystem) {
		t.Error("首行 system 快照不得改写")
	}
	if !strings.Contains(lines[3], "q2") {
		t.Errorf("末行应为新消息: %q", lines[3])
	}
}

func mustSessionDir(t *testing.T, a *Agent) string {
	t.Helper()
	dir, ok := a.SessionDir()
	if !ok {
		t.Fatal("应有会话目录")
	}
	return dir
}

func TestFreezeStopsSaving(t *testing.T) {
	a := newTestAgent(t)
	a.history = append(a.history, Message{Role: "user", Content: "q1"})
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	file := a.SessionFile()
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	a.FreezeSession()
	a.history = append(a.history, Message{Role: "user", Content: "q2"})
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("冻结后不得再写盘")
	}
	a.UnfreezeSession()
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	after2, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(after2) == string(after) {
		t.Fatal("解冻后应恢复写入")
	}
}

func TestAppendNotice(t *testing.T) {
	a := newTestAgent(t)
	if err := a.AppendNotice(MsgTakenOverNotice); err != nil {
		t.Fatal(err)
	}
	h := a.History()
	if len(h) != 1 || h[0].Role != "user" || h[0].Content != MsgTakenOverNotice {
		t.Fatalf("事实行异常: %+v", h)
	}
	if file := a.SessionFile(); file == "" {
		t.Fatal("事实行应已落盘")
	}
}

func TestArchiveDirAccessor(t *testing.T) {
	a := newTestAgent(t)
	if dir, ok := a.ArchiveDir(); !ok || filepath.Base(dir) != "archive" {
		t.Fatalf("ArchiveDir 异常: %q %v", dir, ok)
	}
	if a.ApiProtocol() == "" {
		t.Error("ApiProtocol 不应为空")
	}
}
