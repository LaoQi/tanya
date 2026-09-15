package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSessionStoreRotateAndAppend(t *testing.T) {
	s := newSessionStore(t.TempDir(), false)
	s.rotate()
	if !regexp.MustCompile(`^\d{8}-\d{6}\.jsonl$`).MatchString(filepath.Base(s.path())) {
		t.Fatalf("会话文件名格式: %q", s.path())
	}
	msgs := []Message{{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"}}
	if err := s.append(msgs, "sys"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(before), `{"role":"system","content":"sys"}`) {
		t.Errorf("首行应为 system: %q", before)
	}
	if err := s.append(msgs, "sys"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.path())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("无新消息不应追加")
	}
	msgs = append(msgs, Message{Role: "user", Content: "q2"})
	if err := s.append(msgs, "sys"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"content":"sys"`) != 1 {
		t.Errorf("system 行应只写一次: %q", data)
	}
	if !strings.Contains(string(data), `"content":"q2"`) {
		t.Errorf("增量消息未落盘: %q", data)
	}
}

func TestSessionStoreDisabled(t *testing.T) {
	s := newSessionStore(t.TempDir(), true)
	s.rotate()
	if err := s.append([]Message{{Role: "user", Content: "q"}}, "sys"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path()); !os.IsNotExist(err) {
		t.Error("noSave 不应落盘")
	}
	if !s.disabled {
		t.Error("disabled 应保留标记")
	}
}

func TestSessionStoreLoad(t *testing.T) {
	dir := t.TempDir()
	content := `{"role":"system","content":"sys"}` + "\n" +
		`{"role":"user","content":"q1"}` + "\n" +
		`bad json` + "\n" +
		`{"role":"assistant","content":"a1"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "20260101-000000.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newSessionStore(dir, false)
	history, system, err := s.load("20260101-000000")
	if err != nil || system != "sys" {
		t.Fatalf("load: %v system=%q", err, system)
	}
	if len(history) != 1 || history[0].Content != "q1" {
		t.Errorf("坏行应中断解码且不吞已解部分: %+v", history)
	}
	if s.saved != len(history) || !s.systemSaved {
		t.Errorf("载入后状态未就位: saved=%d systemSaved=%v", s.saved, s.systemSaved)
	}
	if s.path() != filepath.Join(dir, "20260101-000000.jsonl") {
		t.Errorf("path: %q", s.path())
	}
	if _, _, err := s.load("nope"); err == nil {
		t.Error("不存在的会话应报错")
	}
	if _, _, err := s.load("../escape"); err == nil {
		t.Error("非法 id 应报错")
	}
}

func TestSessionStoreLoadNoSystemLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.jsonl"), []byte(`{"role":"user","content":"q"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newSessionStore(dir, false)
	history, system, err := s.load("x")
	if err != nil || system != "" || len(history) != 1 {
		t.Fatalf("无 system 行: history=%+v system=%q err=%v", history, system, err)
	}
	if !s.systemSaved {
		t.Error("载入后应标记 system 已处理")
	}
}

func TestSessionStoreListCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-010101.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"标题"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newSessionStore(dir, false)
	list, err := s.list()
	if err != nil || len(list) != 1 || list[0].Summary != "标题" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if list[0].Path != path {
		t.Errorf("Path 未填充: %q, 期望 %q", list[0].Path, path)
	}
	s.cache[list[0].ID] = SessionInfo{ID: list[0].ID, Summary: "缓存值"}
	list, err = s.list()
	if err != nil || list[0].Summary != "缓存值" {
		t.Errorf("文件未变时应复用缓存: %+v %v", list, err)
	}
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"新标题"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	list, err = s.list()
	if err != nil || list[0].Summary != "新标题" {
		t.Errorf("文件变化后应重扫: %+v %v", list, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	list, err = s.list()
	if err != nil || len(list) != 0 {
		t.Errorf("删除后应清理缓存条目: %+v %v", list, err)
	}
}

func TestSessionStoreListMissingDir(t *testing.T) {
	s := newSessionStore(filepath.Join(t.TempDir(), "nope"), false)
	list, err := s.list()
	if err != nil || len(list) != 0 {
		t.Fatalf("目录不存在应返回空列表: %+v %v", list, err)
	}
}
