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
	cfg.GlobalSession = t.TempDir()
	cfg.SystemPrompt = "sys"
	a, err := New(cfg)
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
	b.sessionDir = a.sessionDir
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
	c.sessionDir = a.sessionDir
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
		p := filepath.Join(a.sessionDir, name+".jsonl")
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

func TestWorkspaceID(t *testing.T) {
	id := workspaceID("/home/coco/Project/tanya")
	if !strings.HasPrefix(id, "home-coco-Project-tanya-") {
		t.Errorf("可读前缀异常: %q", id)
	}
	suffix := strings.TrimPrefix(id, "home-coco-Project-tanya-")
	if len(suffix) != 8 {
		t.Errorf("短哈希应为 8 位: %q", id)
	}
	if id != workspaceID("/home/coco/Project/tanya") {
		t.Error("同一路径应生成相同 id")
	}
	if workspaceID("/a/b-c") == workspaceID("/a-b/c") {
		t.Error("碰撞路径的哈希应不同")
	}
	if got := workspaceID("/"); !strings.HasPrefix(got, "root-") {
		t.Errorf("根目录: %q", got)
	}
}

func TestNewSessionPerWorkspace(t *testing.T) {
	a := newTestAgent(t)
	if a.sessionDir == a.cfg.GlobalSession || !strings.HasPrefix(a.sessionDir, a.cfg.GlobalSession+string(filepath.Separator)) {
		t.Errorf("sessionDir 应为 cfg.GlobalSession 下的工作区子目录: %q", a.sessionDir)
	}
	if _, err := os.Stat(a.sessionDir); err != nil {
		t.Errorf("工作区目录未创建: %v", err)
	}
}

func TestResolveSessionDir(t *testing.T) {
	root := t.TempDir()
	cfg := defaultConfig()
	cfg.GlobalSession = filepath.Join(root, "global")

	if got := resolveSessionDir(cfg, root); got != filepath.Join(cfg.GlobalSession, workspaceID(root)) {
		t.Errorf("auto 无 .tanya 应走 global: %q", got)
	}

	if err := os.MkdirAll(filepath.Join(root, ".tanya"), 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(root, ".tanya", "sessions")
	if got := resolveSessionDir(cfg, root); got != local {
		t.Errorf("auto 有 .tanya 应走 local: %q", got)
	}
	cfg.SessionMode = "global"
	if got := resolveSessionDir(cfg, root); got != filepath.Join(cfg.GlobalSession, workspaceID(root)) {
		t.Errorf("global 显式指定应优先: %q", got)
	}
	cfg.SessionMode = "local"
	if got := resolveSessionDir(cfg, root); got != local {
		t.Errorf("local 显式指定: %q", got)
	}
	cfg.SessionMode = "bogus"
	if got := resolveSessionDir(cfg, root); got != local {
		t.Errorf("未知模式应按 auto 处理: %q", got)
	}
}

func TestNewLocalMode(t *testing.T) {
	tmp := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	cfg := defaultConfig()
	cfg.GlobalSession = filepath.Join(tmp, "global")
	cfg.SessionMode = "local"
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(a.sessionDir, filepath.Join(".tanya", "sessions")) {
		t.Errorf("local 模式目录: %q", a.sessionDir)
	}
	if _, err := os.Stat(a.sessionDir); err != nil {
		t.Errorf("目录未创建: %v", err)
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
