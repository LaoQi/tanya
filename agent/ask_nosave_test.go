package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoSaveAskLeavesNoTrace(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回复"})
	cfg := m.config()
	cfg.SessionMode = "global"
	a, err := New(cfg, NoSave(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "问题", nil); err != nil {
		t.Fatal(err)
	}
	if len(a.History()) != 2 {
		t.Fatalf("内存历史应保留 user+assistant: %d", len(a.History()))
	}
	if _, err := os.Stat(a.sessionDir); !os.IsNotExist(err) {
		t.Fatalf("只读模式不应创建会话目录: %v", err)
	}
	entries, err := os.ReadDir(cfg.GlobalSession)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("只读模式不应留下任何文件: %v", entries)
	}
}

func TestSaveCreatesSessionFile(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回复"})
	cfg := m.config()
	cfg.SessionMode = "global"
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "问题", nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(a.sessionDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("默认模式应写入一个会话文件: %v", entries)
	}
}

func TestNoSaveReadsExistingSessions(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回复"}, mockStep{content: "回复二"})
	cfg := m.config()
	cfg.SessionMode = "global"

	warm, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := warm.Ask(context.Background(), "先存一轮", nil); err != nil {
		t.Fatal(err)
	}
	path := warm.sessionPath
	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	a, err := New(cfg, NoSave(true))
	if err != nil {
		t.Fatal(err)
	}
	list, err := a.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("只读模式应能列出既有会话: %d 条", len(list))
	}
	if err := a.LoadSession(id); err != nil {
		t.Fatalf("只读模式应能载入既有会话: %v", err)
	}
	if err := a.Ask(context.Background(), "再问一轮", nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("只读模式不得改动会话文件: size %d->%d", before.Size(), after.Size())
	}
}

func TestNoSaveMissingDirIsEmpty(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回复"})
	cfg := m.config()
	cfg.SessionMode = "global"
	a, err := New(cfg, NoSave(true))
	if err != nil {
		t.Fatal(err)
	}
	list, err := a.ListSessions()
	if err != nil {
		t.Fatalf("目录缺失不应报错: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("目录缺失应返回空列表: %d", len(list))
	}
	if _, err := os.Stat(a.sessionDir); !os.IsNotExist(err) {
		t.Fatalf("只读模式不应创建目录: %v", err)
	}
}
