package agent

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newArchiveStore(t *testing.T) (*sessionStore, string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "sessions")
	archive := filepath.Join(root, "archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return newSessionStore(dir, archive, false), dir, archive
}

func seedSession(t *testing.T, dir, id, content string, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func listIDs(t *testing.T, s *sessionStore) map[string]SessionInfo {
	t.Helper()
	list, err := s.list()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]SessionInfo{}
	for _, si := range list {
		out[si.ID] = si
	}
	return out
}

func volumeIDs(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	ids := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		ids = append(ids, strings.TrimSuffix(filepath.Base(f.Name), ".jsonl"))
	}
	return ids
}

func archiveIDs(rep ArchiveReport) []string {
	ids := make([]string, 0, len(rep.Sessions))
	for _, e := range rep.Sessions {
		ids = append(ids, e.ID)
	}
	return ids
}

func skipIDs(rep ArchiveReport) map[string]string {
	out := map[string]string{}
	for _, s := range rep.Skipped {
		out[s.ID] = s.Reason
	}
	return out
}

const sampleSession = `{"role":"system","content":"sys"}
{"role":"user","content":"第一条问题\n带换行"}
{"role":"assistant","content":"答"}
{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"run_shell","arguments":"{\"command\":\"ls\"}"}}]}
{"role":"tool","tool_call_id":"c1","name":"run_shell","content":"stdout:\na\n"}
{"role":"assistant","content":"收尾","reasoning_items":[{"id":"r1","type":"reasoning","content":"想一下"}]}
`

func TestArchiveVolumeRoundTrip(t *testing.T) {
	s, dir, archiveDir := newArchiveStore(t)
	now := time.Now().Truncate(2 * time.Second)
	mtime := now.Add(-48 * time.Hour)
	path := seedSession(t, dir, "20260101-090000", sampleSession, mtime)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := scanSessionFile(path, "20260101-090000", mtime, archiveSummaryRunes)
	if err != nil {
		t.Fatal(err)
	}

	rep, err := s.archive(ArchiveOptions{Exclude: "20260101-100000", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sessions) != 1 || rep.Sessions[0].ID != "20260101-090000" {
		t.Fatalf("报告条目异常: %+v", rep.Sessions)
	}
	if rep.Sessions[0].Before != int64(len(before)) || rep.RawBytes != int64(len(before)) {
		t.Errorf("原大小异常: %+v raw=%d want=%d", rep.Sessions[0], rep.RawBytes, len(before))
	}
	if !strings.HasPrefix(filepath.Base(rep.Volume), "archive-"+now.Format("20060102-150405")+".zip") {
		t.Errorf("卷名异常: %q", rep.Volume)
	}
	if rep.VolumeBytes == 0 || rep.Sessions[0].After == 0 {
		t.Errorf("卷/条目压缩后大小未回填: %+v bytes=%d", rep.Sessions[0], rep.VolumeBytes)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("归档后源文件应删除: %v", err)
	}

	zr, err := zip.OpenReader(rep.Volume)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 {
		t.Fatalf("卷内条目数: %d", len(zr.File))
	}
	f := zr.File[0]
	if f.Name != "20260101-090000.jsonl" || f.Method != zip.Deflate {
		t.Errorf("条目头异常: %q %v", f.Name, f.Method)
	}
	if !f.Modified.Local().Equal(mtime) {
		t.Errorf("Modified 应取源文件 mtime: got %v want %v", f.Modified.Local(), mtime)
	}
	var vc archiveVolumeComment
	if err := json.Unmarshal([]byte(zr.Comment), &vc); err != nil {
		t.Fatalf("卷注释不可解析: %q %v", zr.Comment, err)
	}
	wd, _ := os.Getwd()
	if vc.V != 1 || vc.Workspace != wd || vc.Sessions != 1 {
		t.Errorf("卷注释异常: %+v", vc)
	}
	if _, err := time.Parse(time.RFC3339, vc.Created); err != nil {
		t.Errorf("created 应为 RFC3339: %q", vc.Created)
	}
	rc, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, before) {
		t.Errorf("entry 数据应与源文件逐字节一致: %d vs %d", len(data), len(before))
	}

	var ec archiveEntryComment
	if err := json.Unmarshal([]byte(f.Comment), &ec); err != nil {
		t.Fatalf("条目注释不可解析: %q %v", f.Comment, err)
	}
	if ec.V != 1 || ec.Msgs != want.Msgs || ec.Summary != want.Summary || ec.Trunc {
		t.Errorf("条目注释与实读扫描不一致: %+v want msgs=%d summary=%q", ec, want.Msgs, want.Summary)
	}
	if len(f.Comment) > zipCommentLimit {
		t.Errorf("条目注释超上限: %d", len(f.Comment))
	}

	got := listIDs(t, s)["20260101-090000"]
	if !got.Archived || got.Msgs != 5 || got.Summary != want.Summary || !got.MetaOK {
		t.Errorf("列表应列出归档条目及其元数据: %+v", got)
	}
	if got.Size != int64(len(before)) {
		t.Errorf("归档项 Size 应为 entry 未压缩字节: %d want %d", got.Size, len(before))
	}
	if got.Path != rep.Volume {
		t.Errorf("归档项 Path 应指所属卷: %q", got.Path)
	}
	if entries, err := os.ReadDir(archiveDir); err != nil || len(entries) != 1 {
		t.Errorf("归档目录应只留卷文件: %v %v", entries, err)
	}
}

func TestArchiveEntryCommentLimits(t *testing.T) {
	long := strings.Repeat("汉", 5000)
	got := archiveEntryCommentJSON(7, long)
	if len(got) > zipCommentLimit {
		t.Fatalf("注释超 4KiB: %d", len(got))
	}
	var c archiveEntryComment
	if err := json.Unmarshal([]byte(got), &c); err != nil {
		t.Fatalf("注释不可解析: %v", err)
	}
	if c.Msgs != 7 || !c.Trunc || len([]rune(c.Summary)) != archiveSummaryRunes {
		t.Errorf("多字节超长 summary 应截断到 %d rune 并标记 trunc: runes=%d trunc=%v", archiveSummaryRunes, len([]rune(c.Summary)), c.Trunc)
	}

	small := archiveEntryCommentLimited(7, long, []int{80, 20, 0}, 4096)
	var sc archiveEntryComment
	if err := json.Unmarshal([]byte(small), &sc); err != nil {
		t.Fatal(err)
	}
	if len([]rune(sc.Summary)) != 80 || !sc.Trunc {
		t.Errorf("降级到 80 rune 未生效: %d %v", len([]rune(sc.Summary)), sc.Trunc)
	}
	tiny := archiveEntryCommentLimited(7, long, []int{200}, 60)
	var tc archiveEntryComment
	if err := json.Unmarshal([]byte(tiny), &tc); err != nil {
		t.Fatal(err)
	}
	if tc.Summary != "" || tc.Trunc || len(tiny) > 60 {
		t.Errorf("仍超上限时应丢弃 summary: %q", tiny)
	}
	if !strings.Contains(tiny, `"msgs":7`) {
		t.Errorf("降级后仍应保留条数: %q", tiny)
	}
	plain := archiveEntryCommentJSON(3, "短摘要")
	var pc archiveEntryComment
	if err := json.Unmarshal([]byte(plain), &pc); err != nil {
		t.Fatalf("普通注释不可解析: %q %v", plain, err)
	}
	if pc.V != archiveCommentV || pc.Msgs != 3 || pc.Summary != "短摘要" || pc.Trunc {
		t.Errorf("普通注释字段异常: %+v", pc)
	}
}

func TestArchiveFilterMatrix(t *testing.T) {
	now := time.Now().Truncate(2 * time.Second)
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	t.Run("OlderThan", func(t *testing.T) {
		s, dir, _ := newArchiveStore(t)
		seedSession(t, dir, "20260101-010000", sampleSession, old)
		seedSession(t, dir, "20260101-020000", sampleSession, recent)
		seedSession(t, dir, "20260101-030000", sampleSession, now)
		rep, err := s.archive(ArchiveOptions{OlderThan: 24 * time.Hour, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if got := archiveIDs(rep); len(got) != 1 || got[0] != "20260101-010000" {
			t.Errorf("只应归档 24h 前的会话: %v", got)
		}
		if len(rep.Skipped) != 0 {
			t.Errorf("时间窗外的会话应由窗口过滤，不进跳过清单: %v", skipIDs(rep))
		}
	})

	t.Run("Keep", func(t *testing.T) {
		s, dir, _ := newArchiveStore(t)
		for _, id := range []string{"20260101-010000", "20260101-020000", "20260101-030000"} {
			seedSession(t, dir, id, sampleSession, old)
		}
		rep, err := s.archive(ArchiveOptions{Keep: 1, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if got := archiveIDs(rep); len(got) != 2 || got[0] != "20260101-020000" || got[1] != "20260101-010000" {
			t.Errorf("Keep=1 应归档除最新外的全部: %v", got)
		}
		if rep.Active != 3 {
			t.Errorf("Active 应为操作前活跃会话总数: %d", rep.Active)
		}
		if _, err := os.Stat(filepath.Join(dir, "20260101-030000.jsonl")); err != nil {
			t.Errorf("保留的最新会话应原样留活动区: %v", err)
		}
	})

	t.Run("IdleGuard", func(t *testing.T) {
		s, dir, _ := newArchiveStore(t)
		seedSession(t, dir, "20260101-010000", sampleSession, now.Add(-time.Minute))
		seedSession(t, dir, "20260101-020000", sampleSession, now.Add(-time.Hour))
		rep, err := s.archive(ArchiveOptions{Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if got := archiveIDs(rep); len(got) != 1 || got[0] != "20260101-020000" {
			t.Errorf("近 5 分钟内修改的会话应被空闲保护跳过: %v", got)
		}
		if r := skipIDs(rep)["20260101-010000"]; r != MsgArchiveSkipIdle {
			t.Errorf("应记空闲跳过原因: %v", skipIDs(rep))
		}
	})

	t.Run("Exclude", func(t *testing.T) {
		s, dir, _ := newArchiveStore(t)
		seedSession(t, dir, "20260101-010000", sampleSession, old)
		seedSession(t, dir, "20260101-020000", sampleSession, old)
		rep, err := s.archive(ArchiveOptions{Exclude: "20260101-020000", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if got := archiveIDs(rep); len(got) != 1 || got[0] != "20260101-010000" {
			t.Errorf("Exclude 未生效: %v", got)
		}
		if _, err := os.Stat(filepath.Join(dir, "20260101-020000.jsonl")); err != nil {
			t.Errorf("被排除的会话应原样保留: %v", err)
		}
	})

	t.Run("DryRun", func(t *testing.T) {
		s, dir, archiveDir := newArchiveStore(t)
		seedSession(t, dir, "20260101-010000", sampleSession, old)
		rep, err := s.archive(ArchiveOptions{DryRun: true, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if !rep.DryRun || len(rep.Sessions) != 1 || rep.Volume != "" || rep.VolumeBytes != 0 {
			t.Errorf("DryRun 报告异常: %+v", rep)
		}
		if rep.Active != 1 {
			t.Errorf("DryRun 应报操作前活跃会话数: %d", rep.Active)
		}
		if _, err := os.Stat(archiveDir); !os.IsNotExist(err) {
			t.Errorf("DryRun 不应创建归档目录: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "20260101-010000.jsonl")); err != nil {
			t.Errorf("DryRun 不应删除源文件: %v", err)
		}
	})

	t.Run("AlreadyArchived", func(t *testing.T) {
		s, dir, _ := newArchiveStore(t)
		seedSession(t, dir, "20260101-010000", sampleSession, old)
		if _, err := s.archive(ArchiveOptions{Now: now}); err != nil {
			t.Fatal(err)
		}
		seedSession(t, dir, "20260101-010000", sampleSession, old)
		rep, err := s.archive(ArchiveOptions{Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Sessions) != 0 {
			t.Errorf("已在卷内的 id 不应重复归档: %v", archiveIDs(rep))
		}
		if r := skipIDs(rep)["20260101-010000"]; r != MsgArchiveSkipArchived {
			t.Errorf("重复 id 应记跳过原因: %v", skipIDs(rep))
		}
	})

	t.Run("NoCandidate", func(t *testing.T) {
		s, _, archiveDir := newArchiveStore(t)
		rep, err := s.archive(ArchiveOptions{Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Sessions) != 0 || rep.Volume != "" {
			t.Errorf("0 候选不应产生卷: %+v", rep)
		}
		if _, err := os.Stat(archiveDir); !os.IsNotExist(err) {
			t.Errorf("0 候选不应创建归档目录: %v", err)
		}
	})

	t.Run("Disabled", func(t *testing.T) {
		s, dir, archiveDir := newArchiveStore(t)
		s.disabled = true
		seedSession(t, dir, "20260101-010000", sampleSession, old)
		if _, err := s.archive(ArchiveOptions{Now: now}); err == nil {
			t.Error("-n 下归档应报错")
		}
		if _, err := os.Stat(archiveDir); !os.IsNotExist(err) {
			t.Errorf("-n 下不应创建归档目录: %v", err)
		}
	})
}

func TestArchiveTempVolumeIgnored(t *testing.T) {
	s, dir, archiveDir := newArchiveStore(t)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "20260101-090000.jsonl", Method: zip.Deflate, Comment: archiveEntryCommentJSON(1, "半成品")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`{"role":"user","content":"半成品"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(archiveDir, "archive-20260101-000000.zip.tmp-123456")
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	seedSession(t, dir, "20260101-010000", sampleSession, time.Now().Add(-time.Hour))
	list, err := s.list()
	if err != nil {
		t.Fatalf("临时卷不应影响列表: %v", err)
	}
	if len(list) != 1 || list[0].ID != "20260101-010000" || list[0].Archived {
		t.Errorf("临时卷是合法 zip，也不应被列入: %+v", list)
	}
	if _, _, err := s.load("20260101-090000"); err == nil {
		t.Error("临时卷不应可载入")
	}
}

func TestListOrderActiveBeforeArchived(t *testing.T) {
	s, dir, _ := newArchiveStore(t)
	now := time.Now().Truncate(2 * time.Second)
	old := now.Add(-72 * time.Hour)
	for _, id := range []string{"20260101-010000", "20260101-020000", "20260101-030000"} {
		seedSession(t, dir, id, sampleSession, old)
	}
	if _, err := s.archive(ArchiveOptions{Exclude: "20260101-020000", Now: now}); err != nil {
		t.Fatal(err)
	}
	list, err := s.list()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"20260101-020000", "20260101-030000", "20260101-010000"}
	if len(list) != len(want) {
		t.Fatalf("列表条数异常: %+v", list)
	}
	for i, id := range want {
		if list[i].ID != id {
			t.Errorf("第 %d 项应为 %s（活动组在前、组内 id 降序）: %+v", i, id, list)
			break
		}
	}
}

func TestArchivedSummaryTruncatedForList(t *testing.T) {
	s, dir, _ := newArchiveStore(t)
	now := time.Now().Truncate(2 * time.Second)
	long := strings.Repeat("长", 60)
	seedSession(t, dir, "20260101-010000", `{"role":"user","content":"`+long+`"}`+"\n", now.Add(-48*time.Hour))
	if _, err := s.archive(ArchiveOptions{Now: now}); err != nil {
		t.Fatal(err)
	}
	got := listIDs(t, s)["20260101-010000"]
	if !got.Archived {
		t.Fatalf("应为归档项: %+v", got)
	}
	if want := strings.Repeat("长", sessionSummaryRunes) + "..."; got.Summary != want {
		t.Errorf("归档项摘要应按展示口径截断：runes=%d %q", len([]rune(got.Summary)), got.Summary)
	}
	if got.Msgs != 1 {
		t.Errorf("归档项条数异常: %+v", got)
	}
}

func TestArchiveCorruptVolume(t *testing.T) {
	now := time.Now().Truncate(2 * time.Second)
	old := now.Add(-48 * time.Hour)

	t.Run("Truncated", func(t *testing.T) {
		s, dir, _ := newArchiveStore(t)
		seedSession(t, dir, "20260101-010000", sampleSession, old)
		rep, err := s.archive(ArchiveOptions{Now: now})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(rep.Volume)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(rep.Volume, data[:len(data)/2], 0o644); err != nil {
			t.Fatal(err)
		}
		list, err := s.list()
		if err != nil {
			t.Fatalf("截断卷不应让列表报错: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("截断卷条目应被丢弃: %+v", list)
		}
		if _, _, err := s.load("20260101-010000"); err == nil {
			t.Error("载入损坏卷应报错")
		}
	})

	t.Run("CRCMismatch", func(t *testing.T) {
		s, _, archiveDir := newArchiveStore(t)
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			t.Fatal(err)
		}
		payload := `{"role":"user","content":"hello"}` + "\n"
		volume := filepath.Join(archiveDir, "archive-20260101-000000.zip")
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "20260101-010000.jsonl", Method: zip.Store, Comment: archiveEntryCommentJSON(1, "hello")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		raw := buf.Bytes()
		idx := bytes.Index(raw, []byte("hello"))
		if idx < 0 {
			t.Fatal("未找到数据区")
		}
		raw[idx] = 'k'
		if err := os.WriteFile(volume, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		list, err := s.list()
		if err != nil {
			t.Fatalf("CRC 损坏不应让列表报错: %v", err)
		}
		if len(list) != 1 || !list[0].Archived || !list[0].MetaOK {
			t.Fatalf("CD 完好时条目仍应列出: %+v", list)
		}
		if _, _, err := s.load("20260101-010000"); err == nil {
			t.Error("CRC 校验失败应上抛错误")
		}
	})
}

func TestArchiveSameIDPrefersActive(t *testing.T) {
	s, dir, _ := newArchiveStore(t)
	now := time.Now().Truncate(2 * time.Second)
	seedSession(t, dir, "20260101-010000", sampleSession, now.Add(-48*time.Hour))
	if _, err := s.archive(ArchiveOptions{Now: now}); err != nil {
		t.Fatal(err)
	}
	active := `{"role":"user","content":"新的活动会话"}
`
	seedSession(t, dir, "20260101-010000", active, now.Add(-time.Hour))
	list, err := s.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("同 id 双区应只列一条: %+v", list)
	}
	if list[0].Archived || list[0].Msgs != 1 || list[0].Summary != "新的活动会话" {
		t.Errorf("同 id 应取活动项: %+v", list[0])
	}
	if _, _, err := s.load("20260101-010000"); err != nil {
		t.Fatal(err)
	}
	if s.frozen {
		t.Error("载入活动会话不应进入只读态")
	}
}

func TestArchiveReadOnlyLoadKeepsVolumeIntact(t *testing.T) {
	s, dir, archiveDir := newArchiveStore(t)
	now := time.Now().Truncate(2 * time.Second)
	seedSession(t, dir, "20260101-010000", sampleSession, now.Add(-48*time.Hour))
	rep, err := s.archive(ArchiveOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(rep.Volume)
	if err != nil {
		t.Fatal(err)
	}

	history, system, err := s.load("20260101-010000")
	if err != nil {
		t.Fatal(err)
	}
	if system != "sys" || len(history) != 5 {
		t.Fatalf("归档载入内容异常: system=%q msgs=%d", system, len(history))
	}
	if history[0].Content != "第一条问题\n带换行" {
		t.Errorf("载入应保留原始内容: %q", history[0].Content)
	}
	if !s.frozen || s.frozenID != "20260101-010000" || s.path() != "" || s.id() != "" {
		t.Errorf("只读态标记异常: frozen=%v id=%q path=%q", s.frozen, s.id(), s.path())
	}
	if err := s.append(append(history, Message{Role: "user", Content: "继续"}), "sys"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "20260101-010000.jsonl")); !os.IsNotExist(err) {
		t.Error("只读态不应重建源文件")
	}
	fi2, err := os.Stat(rep.Volume)
	if err != nil {
		t.Fatal(err)
	}
	if !fi2.ModTime().Equal(fi.ModTime()) || fi2.Size() != fi.Size() {
		t.Errorf("只读载入不应改动卷: %v/%d → %v/%d", fi.ModTime(), fi.Size(), fi2.ModTime(), fi2.Size())
	}
	if entries, err := os.ReadDir(archiveDir); err != nil || len(entries) != 1 {
		t.Errorf("归档目录清单不应变化: %v %v", entries, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("会话目录不应新增文件: %v %v", entries, err)
	}
}

func TestAskRejectsArchiveReadOnly(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回复"})
	cfg := m.config()
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(2 * time.Second)
	seedSession(t, a.store.dir, "20260101-010000", sampleSession, now.Add(-48*time.Hour))
	if _, err := a.ArchiveSessions(ArchiveOptions{Exclude: a.SessionID(), Now: now}); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadSession("20260101-010000"); err != nil {
		t.Fatal(err)
	}
	id, ok := a.ArchiveReadOnly()
	if !ok || id != "20260101-010000" {
		t.Fatalf("归档只读态标记: %q %v", id, ok)
	}
	if a.SessionFile() != "" {
		t.Errorf("归档只读态不应报会话文件: %q", a.SessionFile())
	}
	before := len(a.History())
	if err := a.Ask(context.Background(), "你好", nil); err != ErrArchiveReadOnly {
		t.Errorf("Ask 应拒绝归档只读对话: %v", err)
	}
	if len(a.History()) != before {
		t.Error("被拒的对话不应进入 history")
	}
	if st := a.Stats(); st.Archived != "20260101-010000" {
		t.Errorf("Stats 应带归档 id: %+v", st)
	}
	if len(m.reqs) != 0 {
		t.Error("被拒的对话不应发起请求")
	}
}

func TestForkFromArchived(t *testing.T) {
	isolatePromptEnv(t)
	m := newMockLLM(t, mockStep{content: "回复"})
	cfg := m.config()
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(2 * time.Second)
	seedSession(t, a.store.dir, "20260101-010000", sampleSession, now.Add(-48*time.Hour))
	if _, err := a.ArchiveSessions(ArchiveOptions{Exclude: a.SessionID(), Now: now}); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadSession("20260101-010000"); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte("# 项目说明（AGENTS.md）\n\nMARKER-AGENTS\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	id, err := a.Fork()
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || id == "20260101-010000" {
		t.Fatalf("fork 应产生新 id: %q", id)
	}
	if _, ok := a.ArchiveReadOnly(); ok {
		t.Error("fork 后应退出归档只读态")
	}
	if a.NoSave() {
		t.Fatal("测试应处于可写模式")
	}
	path := a.store.path()
	if filepath.Base(path) != id+".jsonl" {
		t.Fatalf("fork 应立即落盘: %q", path)
	}
	lines := readLines(t, path)
	if len(lines) != 6 {
		t.Fatalf("fork 文件应为 system + 5 条历史: %d", len(lines))
	}
	var sys Message
	if err := json.Unmarshal([]byte(lines[0]), &sys); err != nil {
		t.Fatal(err)
	}
	if sys.Role != "system" || !strings.Contains(sys.Content, "MARKER-AGENTS") {
		t.Errorf("fork 应取当前 AGENTS.md 快照: %q", sys.Content)
	}
	if a.systemPrompt() != sys.Content {
		t.Errorf("首行应与当前 system 一致: %q vs %q", sys.Content, a.systemPrompt())
	}
	if len(a.History()) != 5 {
		t.Errorf("fork 应继承历史: %d", len(a.History()))
	}

	a.history = append(a.history, Message{Role: "user", Content: "继续"})
	a.history = append(a.history, Message{Role: "assistant", Content: "好"})
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	lines = readLines(t, path)
	if len(lines) != 8 || strings.Count(strings.Join(lines, "\n"), `"role":"system"`) != 1 {
		t.Fatalf("fork 后应按普通会话增量追加: %d 行", len(lines))
	}

	if err := a.Ask(context.Background(), "你好", nil); err != nil {
		t.Fatalf("fork 后应能正常对话: %v", err)
	}
	if len(m.reqs) != 1 {
		t.Errorf("fork 后应发起请求: %d", len(m.reqs))
	}
}

func TestForkRejectedWhenNotArchived(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回复"})
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	before := a.store.path()
	if id, err := a.Fork(); err != ErrForkNotArchive || id != "" {
		t.Fatalf("非归档态应拒绝 fork: %q %v", id, err)
	}
	if a.store.path() != before {
		t.Error("被拒的 fork 不应切换会话文件")
	}
}

func TestSuggestArchive(t *testing.T) {
	newAgent := func(t *testing.T, enabled bool, threshold, keep, sessions int, opts ...Option) *Agent {
		t.Helper()
		isolatePromptEnv(t)
		cfg := defaultConfig()
		cfg.DataDir = t.TempDir()
		cfg.AutoArchive = enabled
		cfg.ArchiveThreshold = threshold
		cfg.ArchiveKeep = keep
		a, err := New(cfg, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(a.store.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		mtime := time.Now().Add(-48 * time.Hour)
		for i := 0; i < sessions; i++ {
			seedSession(t, a.store.dir, fmt.Sprintf("202601%02d-000000", i), sampleSession, mtime)
		}
		return a
	}

	t.Run("BelowThreshold", func(t *testing.T) {
		a := newAgent(t, true, 4, 2, 3)
		if sug, ok := a.SuggestArchive(); ok {
			t.Errorf("未达阈值不应建议归档: %+v", sug)
		}
	})

	t.Run("AtThreshold", func(t *testing.T) {
		a := newAgent(t, true, 4, 2, 4)
		sug, ok := a.SuggestArchive()
		if !ok {
			t.Fatal("达到阈值应建议归档")
		}
		if sug.Threshold != 4 || sug.Keep != 2 || sug.Active != 4 || sug.Candidates != 2 || sug.Bytes <= 0 {
			t.Errorf("建议内容异常: %+v", sug)
		}
		rep, err := a.ArchiveSessions(ArchiveOptions{Keep: sug.Keep, Exclude: a.SessionID()})
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Sessions) != 2 {
			t.Errorf("应归档 2 个会话: %+v", rep.Sessions)
		}
		active, archived := 0, 0
		list, err := a.ListSessions()
		if err != nil {
			t.Fatal(err)
		}
		for _, si := range list {
			if si.Archived {
				archived++
			} else {
				active++
			}
		}
		if active != 2 || archived != 2 {
			t.Errorf("归档后应保留 2 个活跃: active=%d archived=%d", active, archived)
		}
	})

	t.Run("DisabledByConfig", func(t *testing.T) {
		a := newAgent(t, false, 4, 2, 8)
		if sug, ok := a.SuggestArchive(); ok {
			t.Errorf("auto_archive 关闭时不应建议: %+v", sug)
		}
	})

	t.Run("NoSaveMode", func(t *testing.T) {
		a := newAgent(t, true, 4, 2, 8, NoSave(true))
		if sug, ok := a.SuggestArchive(); ok {
			t.Errorf("-n 下不应建议归档: %+v", sug)
		}
	})

	t.Run("CurrentExcludedFromCandidates", func(t *testing.T) {
		a := newAgent(t, true, 3, 1, 0)
		cur := a.SessionID()
		if cur == "" {
			t.Fatal("可写模式应有会话 id")
		}
		mtime := time.Now().Add(-48 * time.Hour)
		seedSession(t, a.store.dir, "99999999-999999", sampleSession, mtime)
		seedSession(t, a.store.dir, cur, sampleSession, mtime)
		seedSession(t, a.store.dir, "00000000-000000", sampleSession, mtime)
		sug, ok := a.SuggestArchive()
		if !ok {
			t.Fatal("达到阈值应建议归档")
		}
		if sug.Active != 3 || sug.Candidates != 1 {
			t.Errorf("当前会话在最旧一侧应占 keep 名额后从候选剔除: %+v", sug)
		}
		rep, err := a.ArchiveSessions(ArchiveOptions{Keep: 1, Exclude: cur})
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Sessions) != 1 || rep.Sessions[0].ID != "00000000-000000" {
			t.Errorf("应只归档非当前的最旧会话: %+v", rep.Sessions)
		}
	})

	t.Run("KeepZero", func(t *testing.T) {
		a := newAgent(t, true, 3, 0, 3)
		sug, ok := a.SuggestArchive()
		if !ok {
			t.Fatal("达到阈值应建议归档")
		}
		if sug.Candidates != 3 {
			t.Errorf("keep=0 应全部入选: %+v", sug)
		}
		rep, err := a.ArchiveSessions(ArchiveOptions{Keep: 0})
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Sessions) != 3 {
			t.Errorf("keep=0 应归档全部: %+v", rep.Sessions)
		}
	})
}
