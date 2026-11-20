package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/battlelab/agent"
	"github.com/LaoQi/tanya/attach"
)

func si(id string, mt time.Time, archived bool) agent.SessionInfo {
	return agent.SessionInfo{ID: id, ModTime: mt, Archived: archived}
}

func TestPickLastEmpty(t *testing.T) {
	if _, ok := PickLast(nil); ok {
		t.Error("空列表不应有候选")
	}
}

func TestPickLastArchivedOnly(t *testing.T) {
	now := time.Now()
	list := []agent.SessionInfo{
		si("20260101-000000", now.Add(-2*time.Hour), true),
		si("20260101-000001", now, true),
	}
	if got, ok := PickLast(list); ok {
		t.Errorf("归档项不参与候选，却选出了 %q", got.ID)
	}
}

// TestPickLastByModTime 钉住口径：最近使用按 mtime，而不是 id。
// 场景取自真实分叉——/load 老会话后续写，其 mtime 最新但 id 更小。
func TestPickLastByModTime(t *testing.T) {
	now := time.Now()
	list := []agent.SessionInfo{
		si("20261119-100000", now.Add(-24*time.Hour), false), // 新建的空会话
		si("20260918-100000", now, false),                    // 刚被续写的老会话
	}
	got, ok := PickLast(list)
	if !ok || got.ID != "20260918-100000" {
		t.Fatalf("应按 mtime 选老会话，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickLastTieBreaksByID(t *testing.T) {
	mt := time.Now()
	list := []agent.SessionInfo{
		si("20260101-000000", mt, false),
		si("20260101-000002", mt, false),
		si("20260101-000001", mt, false),
	}
	got, _ := PickLast(list)
	if got.ID != "20260101-000002" {
		t.Errorf("同 mtime 应取 id 最大者，得到 %q", got.ID)
	}
}

func TestPickLastSkipsArchivedEvenIfNewest(t *testing.T) {
	now := time.Now()
	list := []agent.SessionInfo{
		si("20260101-000000", now.Add(-time.Hour), false),
		si("20260101-000009", now, true),
	}
	got, ok := PickLast(list)
	if !ok || got.ID != "20260101-000000" {
		t.Fatalf("归档项更新也必须排除，得到 %q", got.ID)
	}
}

func TestSessionHolders(t *testing.T) {
	rows := []attach.Row{
		{Entry: attach.Entry{PID: 1, Session: "A"}, Alive: true},
		{Entry: attach.Entry{PID: 2, Session: "A"}, Alive: false}, // 陈旧
		{Entry: attach.Entry{PID: 3, Session: "B"}, Alive: true},
	}
	got := SessionHolders(rows, "A")
	if len(got) != 1 || got[0].PID != 1 {
		t.Fatalf("只应返回活体持有者 pid 1，得到 %+v", got)
	}
	if got := SessionHolders(rows, "C"); len(got) != 0 {
		t.Errorf("无人持有应为空，得到 %+v", got)
	}
	if got := SessionHolders(rows, ""); len(got) != 0 {
		t.Errorf("空 id 不应匹配任何实例，得到 %+v", got)
	}
}

// TestResumeReportsLoadedSession 是接线用例：WithResume 应在欢迎屏之后打出载入报告。
func TestResumeReportsLoadedSession(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", `{"role":"user","content":"旧"}`+"\n")
	if err := a.LoadSession("20260101-100000"); err != nil {
		t.Fatal(err)
	}

	out, errb := &syncBuf{}, &syncBuf{}
	r, err := NewREPL(a, "› ", WithStreams(NewStreams(out, errb, modeRich)), WithProfile(plainProf), WithConsole(newFakeTerm()), WithResume("20260101-100000"))
	if err != nil {
		t.Fatal(err)
	}
	r.st.out.emit(KindDecor, welcomeText())
	if r.resumeID != "" {
		r.st.out.emit(KindNotice, r.loadNotice(r.resumeID))
	}
	got := out.String()
	if want := "已载入会话 20260101-100000"; !strings.Contains(got, want) {
		t.Errorf("输出应含 %q:\n%s", want, got)
	}
}

// TestResumeBeforeAutoArchive 钉住顺序不变量：被恢复的会话必须在自动归档的排除面上，
// 即使它是 mtime 最老的一个（autoArchivePrompt 的 Exclude 取当前会话 id）。
func TestResumeBeforeAutoArchive(t *testing.T) {
	dir := t.TempDir()
	a := newAutoArchiveAgent(t, dir, 4, 2)
	seedAutoArchiveSessions(t, dir, 4)
	target := "202601a-000000"
	sessDir, ok := a.SessionDir()
	if !ok {
		t.Fatal("应有会话目录")
	}
	path := filepath.Join(sessDir, target+".jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("种子文件应存在: %v", err)
	}
	if err := a.LoadSession(target); err != nil {
		t.Fatal(err)
	}
	out, errb := &syncBuf{}, &syncBuf{}
	r, err := NewREPL(a, "› ", WithStreams(NewStreams(out, errb, modeRich)), WithProfile(plainProf), WithConsole(newFakeTerm()))
	if err != nil {
		t.Fatal(err)
	}
	r.autoArchivePrompt()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("当前（恢复的）会话不应被归档: %v", err)
	}
}
