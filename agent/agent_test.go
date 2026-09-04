package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	cfg := defaultConfig()
	cfg.SessionDir = t.TempDir()
	cfg.SystemPrompt = "sys"
	a, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestEstimateTokens(t *testing.T) {
	if got := estimateTokens("你好"); got != 2 {
		t.Errorf("中文: got %d", got)
	}
	if got := estimateTokens("abc"); got != 1 {
		t.Errorf("英文: got %d", got)
	}
	if got := estimateTokens(""); got != 0 {
		t.Errorf("空: got %d", got)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	a := newTestAgent(t)
	a.history = []Message{
		{Role: "user", Content: "问题一"},
		{Role: "assistant", Content: "回答一"},
		{Role: "user", Content: "问题二"},
		{Role: "assistant", Content: "回答二"},
	}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSuffix(filepath.Base(a.sessionPath), ".jsonl")

	b := newTestAgent(t)
	b.cfg.SessionDir = a.cfg.SessionDir
	if err := b.LoadSession(id); err != nil {
		t.Fatal(err)
	}
	if len(b.history) != 4 {
		t.Fatalf("载入消息数: %d", len(b.history))
	}
	for i := range a.history {
		if a.history[i].Content != b.history[i].Content || a.history[i].Role != b.history[i].Role {
			t.Errorf("第 %d 条不一致", i)
		}
	}

	b.history = append(b.history, Message{Role: "user", Content: "问题三"})
	if err := b.save(); err != nil {
		t.Fatal(err)
	}
	c := newTestAgent(t)
	c.cfg.SessionDir = a.cfg.SessionDir
	if err := c.LoadSession(id); err != nil {
		t.Fatal(err)
	}
	if len(c.history) != 5 {
		t.Errorf("追加保存后应为 5 条: %d", len(c.history))
	}
}

func TestLoadSessionInvalid(t *testing.T) {
	a := newTestAgent(t)
	if err := a.LoadSession("../etc/passwd"); err == nil {
		t.Error("路径穿越应被拒绝")
	}
	if err := a.LoadSession("not-exist"); err == nil {
		t.Error("不存在的会话应报错")
	}
}

func TestListSessions(t *testing.T) {
	a := newTestAgent(t)
	write := func(name, content string) {
		p := filepath.Join(a.cfg.SessionDir, name+".jsonl")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("20260101-100000", `{"role":"user","content":"第一个会话"}`+"\n"+`{"role":"assistant","content":"好"}`+"\n")
	write("20260102-100000", `{"role":"user","content":"第二个会话"}`+"\n")

	list, err := a.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("会话数: %d", len(list))
	}
	if list[0].ID != "20260102-100000" {
		t.Errorf("应按 id 倒序: %s", list[0].ID)
	}
	if list[1].Msgs != 2 || list[1].Summary != "第一个会话" {
		t.Errorf("会话信息异常: %+v", list[1])
	}
}

func TestContextInfo(t *testing.T) {
	a := newTestAgent(t)
	a.history = append(a.history, Message{Role: "user", Content: "hi"})
	info := a.ContextInfo()
	if !strings.Contains(info, "消息: 1 条") || !strings.Contains(info, "token") {
		t.Errorf("got %q", info)
	}
}

func TestSetModel(t *testing.T) {
	a := newTestAgent(t)
	a.SetModel("new-model")
	if a.Model() != "new-model" {
		t.Error("模型切换失败")
	}
}
