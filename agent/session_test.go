package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSessionStoreRotateAndAppend(t *testing.T) {
	s := newSessionStore(t.TempDir(), "", "", false)
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
	s := newSessionStore(t.TempDir(), "", "", true)
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
	s := newSessionStore(dir, "", "", false)
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
	s := newSessionStore(dir, "", "", false)
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
	s := newSessionStore(dir, "", "", false)
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
	s := newSessionStore(filepath.Join(t.TempDir(), "nope"), "", "", false)
	list, err := s.list()
	if err != nil || len(list) != 0 {
		t.Fatalf("目录不存在应返回空列表: %+v %v", list, err)
	}
}

func TestSessionIDAndFile(t *testing.T) {
	a := newTestAgent(t)
	id := a.SessionID()
	if id == "" || strings.ContainsAny(id, "/\\") || strings.HasSuffix(id, ".jsonl") {
		t.Fatalf("会话 id 异常: %q", id)
	}
	if got := a.SessionFile(); got != "" {
		t.Errorf("未落盘不应报文件: %q", got)
	}
	a.history = []Message{{Role: "user", Content: "q"}}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	if got, want := a.SessionFile(), a.Stats().Session; got != want {
		t.Errorf("落盘后文件: got %q want %q", got, want)
	}
	if filepath.Base(a.SessionFile()) != id+".jsonl" {
		t.Errorf("id 与文件名不一致: %q vs %q", id, a.SessionFile())
	}
}

func TestSessionIDAndFileNoSave(t *testing.T) {
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	a, err := New(cfg, NoSave(true))
	if err != nil {
		t.Fatal(err)
	}
	if a.SessionID() != "" || a.SessionFile() != "" {
		t.Errorf("只读模式不应有 id/文件: %q %q", a.SessionID(), a.SessionFile())
	}
	if a.NoSave() != true {
		t.Error("NoSave 应为真")
	}
}

func TestSessionStoreIDEmpty(t *testing.T) {
	if got := (&sessionStore{}).id(); got != "" {
		t.Errorf("未 rotate 的 store 不应有 id: %q", got)
	}
}

func TestAskFlushesToolRoundsIncrementally(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "calc", args: `{"expression":"1+1"}`}}},
		mockStep{toolCalls: []mockToolCall{{id: "call_2", name: "probe", args: `{}`}}},
		mockStep{content: "done"},
	)
	var a *Agent
	snapshot := make(chan string, 1)
	probe := NewTool("probe", "读会话文件", `{"type":"object"}`,
		func(ctx context.Context, args string) ToolResult {
			b, err := os.ReadFile(a.SessionFile())
			if err != nil {
				t.Errorf("读会话文件: %v", err)
			}
			snapshot <- string(b)
			return ToolResult{Text: "ok"}
		})
	a = newAgent(t, m, WithTools(probe))
	if err := a.Ask(context.Background(), "跑两轮", nil); err != nil {
		t.Fatal(err)
	}
	snap := <-snapshot
	for _, want := range []string{
		`"role":"user","content":"跑两轮"`,
		`"role":"assistant"`,
		`"name":"calc"`,
		`"role":"tool"`,
	} {
		if !strings.Contains(snap, want) {
			t.Errorf("增量落盘缺失 %s:\n%s", want, snap)
		}
	}
}

func toolCall(id, name, args string) ToolCall {
	var tc ToolCall
	tc.ID = id
	tc.Type = "function"
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

func TestExportSessionRoundTrip(t *testing.T) {
	a := newTestAgent(t)
	a.history = []Message{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1", ToolCalls: []ToolCall{toolCall("t1", "run_shell", `{"command":"ls"}`)}},
		{Role: "tool", Content: "out", ToolCallID: "t1", Name: "run_shell"},
		{Role: "user", Content: "看图", Images: []ImageRef{{MIME: "image/png", Data: "QUJD"}}},
	}
	path := filepath.Join(t.TempDir(), "recover.jsonl")
	n, err := a.ExportSession(path)
	if err != nil || n != 4 {
		t.Fatalf("ExportSession: n=%d err=%v", n, err)
	}
	s := newSessionStore(filepath.Dir(path), "", "", false)
	history, system, err := s.load(strings.TrimSuffix(filepath.Base(path), ".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if system != a.prompt.system() {
		t.Errorf("system 首行不一致: %q", system)
	}
	if len(history) != 4 {
		t.Fatalf("往返条数: %d", len(history))
	}
	if len(history[1].ToolCalls) != 1 || history[1].ToolCalls[0].Function.Name != "run_shell" {
		t.Errorf("tool_calls 丢失: %+v", history[1])
	}
	if history[2].ToolCallID != "t1" || history[2].Name != "run_shell" {
		t.Errorf("tool 字段丢失: %+v", history[2])
	}
	if len(history[3].Images) != 1 || history[3].Images[0].Data != "QUJD" {
		t.Errorf("图像字段丢失: %+v", history[3])
	}
}

func TestExportSessionDoesNotTouchSessionFile(t *testing.T) {
	a := newTestAgent(t)
	a.history = []Message{{Role: "user", Content: "q1"}}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile(a.store.path())
	if err != nil {
		t.Fatal(err)
	}
	saved, expected := a.store.saved, a.store.systemSaved
	out := filepath.Join(t.TempDir(), "r.jsonl")
	if _, err := a.ExportSession(out); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(a.store.path())
	if err != nil {
		t.Fatal(err)
	}
	if string(orig) != string(after) {
		t.Error("导出不应改动会话文件")
	}
	if a.store.saved != saved || a.store.systemSaved != expected {
		t.Error("导出不应改动落盘游标")
	}
}

func TestExportSessionEmptyHistory(t *testing.T) {
	a := newTestAgent(t)
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	n, err := a.ExportSession(path)
	if err != nil || n != 0 {
		t.Fatalf("空历史导出: n=%d err=%v", n, err)
	}
	s := newSessionStore(filepath.Dir(path), "", "", false)
	history, system, err := s.load(strings.TrimSuffix(filepath.Base(path), ".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 || system != a.prompt.system() {
		t.Errorf("空历史往返: history=%d system=%q", len(history), system)
	}
}

func TestExportSessionNoPath(t *testing.T) {
	a := newTestAgent(t)
	if _, err := a.ExportSession("  "); err == nil {
		t.Error("空路径应报错")
	}
}

func TestExportSessionBadPathLeavesNoPartial(t *testing.T) {
	a := newTestAgent(t)
	a.history = []Message{{Role: "user", Content: "q"}}
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "r.jsonl")
	if _, err := a.ExportSession(target); err == nil {
		t.Fatal("目录不存在应报错")
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Error("失败不应留下 .tmp")
	}
}

func TestSessionStoreAppendNoSilentRecreate(t *testing.T) {
	dir := t.TempDir()
	s := newSessionStore(dir, "", "", false)
	s.rotate()
	path := s.path()
	msgs := []Message{{Role: "user", Content: "q1"}}
	if err := s.append(msgs, "sys"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	msgs = append(msgs, Message{Role: "assistant", Content: "a1"})
	err := s.append(msgs, "sys")
	if err == nil {
		t.Fatal("文件消失后追加应报错")
	}
	if !strings.Contains(err.Error(), "会话文件已丢失") {
		t.Errorf("错误信息应点明文件丢失: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("不应静默重建会话文件")
	}
}

func TestSessionStoreAppendCreatesOnFirstWrite(t *testing.T) {
	dir := t.TempDir()
	s := newSessionStore(dir, "", "", false)
	s.rotate()
	if _, err := os.Stat(s.path()); !os.IsNotExist(err) {
		t.Fatal("rotate 不应建文件")
	}
	if err := s.append([]Message{{Role: "user", Content: "a"}}, "sys"); err != nil {
		t.Fatalf("首次写入应创建文件: %v", err)
	}
	if err := s.append([]Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"}}, "sys"); err != nil {
		t.Fatalf("文件存在后应可续写: %v", err)
	}
	data, err := os.ReadFile(s.path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"role":"system"`) != 1 {
		t.Errorf("system 行应只写一次: %q", data)
	}
}

func TestSessionStoreAppendAfterLoadFailsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	content := `{"role":"system","content":"sys"}` + "\n" + `{"role":"user","content":"q"}` + "\n"
	path := filepath.Join(dir, "20260101-000000.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newSessionStore(dir, "", "", false)
	history, _, err := s.load("20260101-000000")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	history = append(history, Message{Role: "assistant", Content: "a"})
	if err := s.append(history, "sys"); err == nil {
		t.Error("载入的文件消失后追加应报错")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("不应静默重建")
	}
}

func TestSessionStoreAppendAfterLoadSystemOnlyFailsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-000000.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"system","content":"sys"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newSessionStore(dir, "", "", false)
	history, _, err := s.load("20260101-000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 || s.saved != 0 {
		t.Fatalf("仅 system 行应得空历史: %d saved=%d", len(history), s.saved)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := s.append([]Message{{Role: "user", Content: "q"}}, "sys"); err == nil {
		t.Error("载入（即使历史为空）后文件消失应报错，不得重建")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("不应静默重建")
	}
}

func TestAskFailsWhenSessionFileDeleted(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "答一"}, mockStep{content: "答二"})
	a := newAgent(t, m)
	if err := a.Ask(context.Background(), "第一问", nil); err != nil {
		t.Fatal(err)
	}
	path := a.store.path()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	err := a.Ask(context.Background(), "第二问", nil)
	if err == nil {
		t.Fatal("会话文件消失后 Ask 应返回错误，不得静默成功")
	}
	if !strings.Contains(err.Error(), "会话文件已丢失") {
		t.Errorf("错误应点明文件丢失: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("不应静默重建会话文件")
	}
}

func TestSettleTurnStillRunsWhenFileDeleted(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "答一"}, mockStep{content: "答二"})
	a := newAgent(t, m)
	if err := a.Ask(context.Background(), "第一问", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a.store.path()); err != nil {
		t.Fatal(err)
	}
	var end TurnInfo
	sink := EventSink(func(e Event) {
		if e.Kind == EventTurnEnd {
			end = e.Turn
		}
	})
	_ = a.Ask(context.Background(), "第二问", sink)
	if !end.Failed {
		t.Error("文件丢失导致的回合失败应反映在 EventTurnEnd.Failed")
	}
}
