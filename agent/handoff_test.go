package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newHandoffAgent(t *testing.T, m *mockLLM, opts ...Option) *Agent {
	t.Helper()
	opts = append([]Option{WithSystemPrompt(testBasePrompt)}, opts...)
	return newAgentWithCfg(t, m.config(), opts...)
}

func readSessionMsgs(t *testing.T, path string) []Message {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var msgs []Message
	for {
		var m Message
		if err := dec.Decode(&m); err != nil {
			break
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func handoffArgs(summary string, cont *bool) string {
	m := map[string]any{"summary": summary}
	if cont != nil {
		m["continue"] = *cont
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestHandoffSwitchesSession(t *testing.T) {
	m := newMockLLM(t,
		mockStep{
			toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgs("进度：做完了 A\n下一步：做 B", nil)}},
			usage:     &Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
		},
		mockStep{content: "已交接", usage: &Usage{PromptTokens: 150, CompletionTokens: 5, TotalTokens: 155}},
	)
	a := newHandoffAgent(t, m)
	oldFile := a.store.path()
	oldID := a.SessionID()

	if err := a.Ask(context.Background(), "干活", nil); err != nil {
		t.Fatal(err)
	}

	h, ok := a.TakeHandoff()
	if !ok {
		t.Fatal("应产生交接")
	}
	if h.OldID != oldID {
		t.Errorf("旧 id = %q, 期望 %q", h.OldID, oldID)
	}
	if h.NewID == "" || h.NewID == oldID {
		t.Fatalf("新 id 异常: %q（旧 %q）", h.NewID, oldID)
	}
	if h.NewID != a.SessionID() {
		t.Errorf("交接后当前会话应为新会话: %q vs %q", h.NewID, a.SessionID())
	}
	if !h.Continue {
		t.Error("continue 缺省应为 true")
	}
	if h.OldMsgs < 3 {
		t.Errorf("旧会话条数异常: %d", h.OldMsgs)
	}
	if h.NoSave {
		t.Error("非 -n 下 NoSave 应为 false")
	}
	if _, ok := a.TakeHandoff(); ok {
		t.Error("TakeHandoff 应一次性取走")
	}

	old := readSessionMsgs(t, oldFile)
	if len(old) != h.OldMsgs+2 {
		t.Fatalf("旧会话应为 system + 旧历史 + 移交事实行: %d 条（旧历史 %d）", len(old), h.OldMsgs)
	}
	if last := old[len(old)-1]; last.Role != "user" || last.Content != "[会话已移交至 "+h.NewID+"]" {
		t.Errorf("旧会话末条应为移交事实行: %+v", last)
	}
	foundTool := false
	for _, msg := range old {
		if msg.Role == "tool" && strings.Contains(msg.Content, "已请求会话交接") {
			foundTool = true
		}
	}
	if !foundTool {
		t.Error("旧会话应留下 next_session 的工具结果")
	}

	fresh := readSessionMsgs(t, a.SessionFile())
	if len(fresh) != 2 {
		t.Fatalf("新会话应为 system + 交接消息 2 条: %d", len(fresh))
	}
	if fresh[0].Role != "system" || fresh[0].Content != testBasePrompt {
		t.Errorf("新会话首行应为当前 system 快照: %+v", fresh[0])
	}
	if fresh[1].Role != "user" || !strings.Contains(fresh[1].Content, "进度：做完了 A") {
		t.Errorf("新会话首条应为交接摘要: %+v", fresh[1])
	}
	if !strings.Contains(fresh[1].Content, oldID) {
		t.Errorf("交接消息应标注来源会话: %+v", fresh[1])
	}
	if got := a.History(); len(got) != 1 || got[0].Role != "user" {
		t.Errorf("内存 history 应只剩交接消息: %+v", got)
	}
	st := a.Stats()
	if st.Messages != 1 {
		t.Errorf("新会话条数统计应归零重算: %d", st.Messages)
	}
	if st.PromptTokens != 0 || st.CompletionTokens != 0 || st.TotalTokens != 0 || st.HasContext {
		t.Errorf("新会话用量统计应归零（旧会话 usage 不得带入）: %+v", st)
	}
}

func TestHandoffContinueFalse(t *testing.T) {
	no := false
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgs("摘要", &no)}}},
		mockStep{content: "已交接"},
	)
	a := newHandoffAgent(t, m)
	if err := a.Ask(context.Background(), "干活", nil); err != nil {
		t.Fatal(err)
	}
	h, ok := a.TakeHandoff()
	if !ok {
		t.Fatal("应产生交接")
	}
	if h.Continue {
		t.Error("continue:false 应被保留")
	}
}

func TestContinueAppendsWithoutUserMessage(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "接着干"})
	a := newHandoffAgent(t, m)
	a.history = []Message{{Role: "user", Content: "摘要"}}
	if err := a.Continue(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	h := a.History()
	if len(h) != 2 {
		t.Fatalf("Continue 应只追加 assistant: %+v", h)
	}
	if h[1].Role != "assistant" || h[1].Content != "接着干" {
		t.Errorf("追加内容异常: %+v", h[1])
	}
}

func TestContinueErrorRollsBack(t *testing.T) {
	m := newMockLLM(t, mockStep{status: 500})
	a := newHandoffAgent(t, m)
	a.history = []Message{{Role: "user", Content: "摘要"}}
	if err := a.Continue(context.Background(), nil); err == nil {
		t.Fatal("应返回错误")
	}
	if len(a.History()) != 1 {
		t.Fatalf("无产出错误应回滚: %+v", a.History())
	}
}

func TestContinueInterruptKeepsPartialTurn(t *testing.T) {
	m := newMockLLM(t, mockStep{toolCalls: []mockToolCall{{id: "c1", name: "run_shell", args: `{"command":"sleep 30"}`}}})
	a := newHandoffAgent(t, m)
	a.history = []Message{{Role: "user", Content: "摘要"}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	err := a.Continue(ctx, nil)
	var ie *InterruptError
	if !errors.As(err, &ie) || !ie.Kept {
		t.Fatalf("应中断并保留产出: %v", err)
	}
	h := a.History()
	if len(h) != 4 {
		t.Fatalf("应保留 摘要user+assistant+tool+中断提示 共4条: %d", len(h))
	}
	if h[len(h)-1].Content != MsgInterruptNotice {
		t.Errorf("末条应为中断提示: %+v", h[len(h)-1])
	}
}

func TestHandoffEmptySummaryRejected(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgs("  \n ", nil)}}},
		mockStep{content: "继续"},
	)
	a := newHandoffAgent(t, m)
	id := a.SessionID()
	if err := a.Ask(context.Background(), "干活", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.TakeHandoff(); ok {
		t.Fatal("空摘要不应产生交接")
	}
	if a.SessionID() != id {
		t.Fatalf("空摘要不应换会话: %q → %q", id, a.SessionID())
	}
	if !strings.Contains(lastToolResult(t, a), MsgHandoffEmpty) {
		t.Errorf("工具结果应报空摘要: %q", lastToolResult(t, a))
	}
}

func TestHandoffTooLongRejected(t *testing.T) {
	a := newTestAgent(t)
	_, err := a.RequestHandoff(strings.Repeat("字", handoffSummaryLimit+1), true, "")
	if err == nil || !strings.Contains(err.Error(), "过长") {
		t.Fatalf("超长摘要应报错: %v", err)
	}
	if a.handoffPend != nil {
		t.Error("校验失败不应置 pending")
	}
}

func TestHandoffDuplicateRequest(t *testing.T) {
	a := newTestAgent(t)
	if _, err := a.RequestHandoff("第一次", true, ""); err != nil {
		t.Fatal(err)
	}
	_, err := a.RequestHandoff("第二次", false, "")
	if err == nil || !strings.Contains(err.Error(), MsgHandoffDup) {
		t.Fatalf("二次请求应报错: %v", err)
	}
	if a.handoffPend.summary != "第一次" {
		t.Errorf("首次请求应生效: %+v", a.handoffPend)
	}
}

func TestHandoffDroppedOnTurnError(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgs("摘要", nil)}}},
		mockStep{status: 500},
		mockStep{content: "下一轮"},
	)
	a := newHandoffAgent(t, m)
	id := a.SessionID()
	if err := a.Ask(context.Background(), "干活", nil); err == nil {
		t.Fatal("应返回错误")
	}
	if a.handoffPend != nil {
		t.Error("错误回合应丢弃 pending")
	}
	if _, ok := a.TakeHandoff(); ok {
		t.Error("错误回合不应产生交接")
	}
	if a.SessionID() != id {
		t.Errorf("错误回合不应换会话: %q → %q", id, a.SessionID())
	}
	if err := a.Continue(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.TakeHandoff(); ok {
		t.Error("陈旧 pending 不应在下一回合生效")
	}
}

func TestHandoffSummaryNormalized(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgs("  第一行\r\n第二行  ", nil)}}},
		mockStep{content: "已交接"},
	)
	a := newHandoffAgent(t, m)
	if err := a.Ask(context.Background(), "干活", nil); err != nil {
		t.Fatal(err)
	}
	h, ok := a.TakeHandoff()
	if !ok {
		t.Fatal("应产生交接")
	}
	if h.Summary != "第一行\n第二行" {
		t.Errorf("摘要应归一换行与首尾空白: %q", h.Summary)
	}
}

func TestHandoffNoSaveKeepsMemoryOnly(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgs("摘要", nil)}}},
		mockStep{content: "已交接"},
	)
	a := newHandoffAgent(t, m, NoSave(true))
	if err := a.Ask(context.Background(), "干活", nil); err != nil {
		t.Fatal(err)
	}
	h, ok := a.TakeHandoff()
	if !ok {
		t.Fatal("应产生交接")
	}
	if !h.NoSave {
		t.Error("-n 下 NoSave 应为 true")
	}
	if h.OldFile != "" || a.SessionFile() != "" {
		t.Error("-n 下不应有会话文件")
	}
	if len(a.History()) != 1 {
		t.Errorf("内存 history 仍应切到交接消息: %+v", a.History())
	}
}

func TestHandoffOldFileUnchangedExceptNotice(t *testing.T) {
	m := newMockLLM(t,
		mockStep{content: "第一轮"},
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgs("摘要", nil)}}},
		mockStep{content: "已交接"},
	)
	a := newHandoffAgent(t, m)
	if err := a.Ask(context.Background(), "第一问", nil); err != nil {
		t.Fatal(err)
	}
	oldFile := a.store.path()
	before := readSessionMsgs(t, oldFile)
	if len(before) != 3 {
		t.Fatalf("第一轮后应有 system+user+assistant: %d", len(before))
	}
	if err := a.Ask(context.Background(), "第二问", nil); err != nil {
		t.Fatal(err)
	}
	after := readSessionMsgs(t, oldFile)
	if len(after) < len(before) {
		t.Fatalf("旧会话不应变短: %d → %d", len(before), len(after))
	}
	for i, msg := range before {
		b1, _ := json.Marshal(msg)
		b2, _ := json.Marshal(after[i])
		if string(b1) != string(b2) {
			t.Fatalf("旧会话第 %d 条被改写: %s → %s", i, b1, b2)
		}
	}
}

func TestHandoffToolDef(t *testing.T) {
	a := newTestAgent(t)
	tool, ok := a.tools.lookup("next_session")
	if !ok {
		t.Fatal("应注册 next_session")
	}
	def := tool.Definition()
	if def.Function.Description == "" || len(def.Function.Parameters) == 0 {
		t.Fatal("描述与参数不应为空")
	}
	if !strings.Contains(string(def.Function.Parameters), "summary") {
		t.Errorf("参数应含 summary: %s", def.Function.Parameters)
	}
}

func TestHandoffToolBadJSON(t *testing.T) {
	a := newTestAgent(t)
	res := newNextTool(a).Invoke(context.Background(), "{bad")
	if !strings.Contains(res.Text, "参数解析失败") {
		t.Errorf("坏 JSON 应报解析失败: %q", res.Text)
	}
}

func TestHandoffStaleStateCleared(t *testing.T) {
	a := newTestAgent(t)
	seed := func() {
		a.handoffPend = &pendingHandoff{summary: "陈旧", cont: true}
		a.lastHandoff = &Handoff{NewID: "陈旧"}
	}
	a.history = []Message{{Role: "user", Content: "q"}}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	id := a.SessionID()
	if id == "" {
		t.Fatal("应有会话 id")
	}
	cases := []struct {
		name  string
		reset func() error
	}{
		{"NewSession", func() error { a.NewSession(); return nil }},
		{"Fork", func() error { _, err := a.Fork(); return err }},
		{"LoadSession", func() error { return a.LoadSession(id) }},
	}
	for _, tc := range cases {
		seed()
		if err := tc.reset(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if a.handoffPend != nil {
			t.Errorf("%s 后不应残留 pending", tc.name)
		}
		if _, ok := a.TakeHandoff(); ok {
			t.Errorf("%s 后不应残留交接快照", tc.name)
		}
	}
}

func TestHandoffSummaryLimitBoundary(t *testing.T) {
	a := newTestAgent(t)
	if _, err := a.RequestHandoff(strings.Repeat("字", handoffSummaryLimit), true, ""); err != nil {
		t.Errorf("恰好等于上限应通过: %v", err)
	}
	b := newTestAgent(t)
	if _, err := b.RequestHandoff(strings.Repeat("字", handoffSummaryLimit/2), true, ""); err != nil {
		t.Errorf("rune 未超限（字节数已超）应按 rune 判定通过: %v", err)
	}
	c := newTestAgent(t)
	_, err := c.RequestHandoff(strings.Repeat("字", handoffSummaryLimit+1), true, "")
	if err == nil || !strings.Contains(err.Error(), "过长") {
		t.Errorf("超一个字符应报过长: %v", err)
	}
}

func TestContinueRejectsArchiveReadOnly(t *testing.T) {
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
	if id, ok := a.ArchiveReadOnly(); !ok || id != "20260101-010000" {
		t.Fatalf("应为归档只读态: %q %v", id, ok)
	}
	if err := a.Continue(context.Background(), nil); err != ErrArchiveReadOnly {
		t.Errorf("Continue 应拒绝归档只读: %v", err)
	}
	if len(m.reqs) != 0 {
		t.Error("被拒的续跑不应发起请求")
	}
}

func handoffArgsWs(summary, workspace string) string {
	m := map[string]any{"summary": summary, "workspace": workspace}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestNextToolDefinitionHasWorkspace(t *testing.T) {
	def := newNextTool(newTestAgent(t)).Definition()
	if !strings.Contains(string(def.Function.Parameters), `"workspace"`) {
		t.Errorf("参数表应含 workspace: %s", def.Function.Parameters)
	}
	if !strings.Contains(def.Function.Description, "workspace") {
		t.Errorf("描述应说明 workspace: %s", def.Function.Description)
	}
}

func TestHandoffSwitchesWorkspace(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	writeAgents(t, filepath.Join(dirA, "AGENTS.md"), "A 项目说明")
	writeAgents(t, filepath.Join(dirB, "AGENTS.md"), "B 项目说明")
	if err := os.MkdirAll(filepath.Join(dirB, ".tanya"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newMockLLM(t,
		mockStep{
			toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgsWs("进度：A 做完\n下一步：去 B", dirB)}},
			usage:     &Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
		},
		mockStep{content: "已交接"},
	)
	a := newHandoffAgent(t, m)
	if err := a.SwitchWorkspace(dirA); err != nil {
		t.Fatal(err)
	}
	oldFile := a.store.path()
	oldID := a.SessionID()
	if err := a.Ask(context.Background(), "干活", nil); err != nil {
		t.Fatal(err)
	}

	h, ok := a.TakeHandoff()
	if !ok {
		t.Fatal("应产生交接")
	}
	if h.Failed {
		t.Fatalf("切换成功不应标记失败: %+v", h)
	}
	if h.OldWorkspace != dirA || h.Workspace != dirB {
		t.Errorf("工作区快照异常: %q → %q", h.OldWorkspace, h.Workspace)
	}
	if a.Workspace() != dirB {
		t.Errorf("交接后工作区应为目标: %q", a.Workspace())
	}
	if h.NewID == "" || h.NewID != a.SessionID() {
		t.Fatalf("新会话 id 异常: %q（旧 %q 当前 %q）", h.NewID, oldID, a.SessionID())
	}
	if dir, ok := a.SessionDir(); !ok || dir != filepath.Join(dirB, ".tanya", "sessions") {
		t.Errorf("新会话应落目标工作区: got %q ok=%v", dir, ok)
	}

	old := readSessionMsgs(t, oldFile)
	if len(old) != h.OldMsgs+2 {
		t.Errorf("旧会话应为 system + 旧历史 + 移交事实行: %d 条（旧历史 %d）", len(old), h.OldMsgs)
	}
	want := "[会话已移交至 " + h.NewID + "（工作区 " + dirB + "）]"
	if last := old[len(old)-1]; last.Role != "user" || last.Content != want {
		t.Errorf("旧会话末条事实行异常: %+v（期望 %q）", last, want)
	}
	foundTool := false
	for _, msg := range old {
		if msg.Role == "tool" && strings.Contains(msg.Content, "工作区切至") {
			foundTool = true
		}
	}
	if !foundTool {
		t.Error("工具结果应说明工作区切至目标")
	}

	fresh := readSessionMsgs(t, a.SessionFile())
	if len(fresh) != 2 {
		t.Fatalf("新会话应为 system + 交接消息: %d", len(fresh))
	}
	if fresh[0].Role != "system" || !strings.Contains(fresh[0].Content, "B 项目说明") {
		t.Errorf("新会话 system 应取目标工作区说明: %+v", fresh[0])
	}
	if strings.Contains(fresh[0].Content, "A 项目说明") {
		t.Errorf("新会话 system 不应残留旧工作区说明: %+v", fresh[0])
	}
	if fresh[1].Role != "user" || !strings.Contains(fresh[1].Content, "进度：A 做完") {
		t.Errorf("新会话首条应为交接摘要: %+v", fresh[1])
	}
	if !strings.Contains(fresh[1].Content, dirA) || !strings.Contains(fresh[1].Content, dirB) {
		t.Errorf("交接消息应标注工作区来源与去向: %+v", fresh[1])
	}
	if !strings.Contains(fresh[1].Content, "工作区已切换") {
		t.Errorf("交接消息应说明工作区已切换: %+v", fresh[1])
	}
	if st := a.Stats(); st.Messages != 1 || st.PromptTokens != 0 || st.HasContext {
		t.Errorf("新会话统计应归零: %+v", st)
	}
}

func TestHandoffWorkspaceSameAsCurrent(t *testing.T) {
	dirA := t.TempDir()
	writeAgents(t, filepath.Join(dirA, "AGENTS.md"), "A 项目说明")
	cases := []struct {
		name string
		arg  string
	}{
		{"绝对路径", dirA},
		{"尾斜杠", dirA + string(filepath.Separator)},
		{"相对当前工作区", "."},
	}
	for _, tc := range cases {
		m := newMockLLM(t,
			mockStep{toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgsWs("摘要", tc.arg)}}},
			mockStep{content: "已交接"},
		)
		a := newHandoffAgent(t, m)
		if err := a.SwitchWorkspace(dirA); err != nil {
			t.Fatal(err)
		}
		oldFile := a.store.path()
		oldID := a.SessionID()
		if err := a.Ask(context.Background(), "干活", nil); err != nil {
			t.Fatal(err)
		}
		h, ok := a.TakeHandoff()
		if !ok || h.Failed {
			t.Fatalf("%s: 应正常交接: %+v ok=%v", tc.name, h, ok)
		}
		if h.OldWorkspace != dirA || h.Workspace != dirA || a.Workspace() != dirA {
			t.Errorf("%s: 同目录不应切换: %+v ws=%q", tc.name, h, a.Workspace())
		}
		if h.NewID == "" || h.NewID == oldID {
			t.Fatalf("%s: 应开启新会话: %q", tc.name, h.NewID)
		}
		old := readSessionMsgs(t, oldFile)
		if last := old[len(old)-1]; last.Content != "[会话已移交至 "+h.NewID+"]" {
			t.Errorf("%s: 未切换的事实行不应带工作区: %+v", tc.name, last)
		}
		for _, msg := range old {
			if msg.Role == "tool" && strings.Contains(msg.Content, "工作区切至") {
				t.Errorf("%s: 工具结果不应提示切换: %s", tc.name, msg.Content)
			}
		}
		fresh := readSessionMsgs(t, a.SessionFile())
		if len(fresh) != 2 || !strings.Contains(fresh[0].Content, "A 项目说明") {
			t.Errorf("%s: 新会话应留在原工作区: %+v", tc.name, fresh)
		}
		if strings.Contains(fresh[1].Content, "工作区已切换") {
			t.Errorf("%s: 未切换不应标注工作区: %+v", tc.name, fresh[1])
		}
	}
}

func TestHandoffBadWorkspaceRejected(t *testing.T) {
	a := newTestAgent(t)
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []string{filepath.Join(t.TempDir(), "missing"), file} {
		res := newNextTool(a).Invoke(context.Background(), handoffArgsWs("摘要", ws))
		if !strings.Contains(res.Text, MsgErrPrefix) || !strings.Contains(res.Text, "不存在或不是目录") {
			t.Errorf("%q 应以工具错误拒绝: %q", ws, res.Text)
		}
		if a.handoffPend != nil {
			t.Fatalf("%q 不应置 pending", ws)
		}
		if len(a.History()) != 0 {
			t.Fatalf("%q 不应改动历史: %+v", ws, a.History())
		}
	}
	if _, err := a.RequestHandoff("摘要", true, "   "); err != nil {
		t.Errorf("空白工作区应视为未指定: %v", err)
	}
	if a.handoffPend == nil || a.handoffPend.workspace != "" {
		t.Errorf("空白工作区不应产生目标: %+v", a.handoffPend)
	}
}

func TestHandoffWorkspaceUnavailableAborts(t *testing.T) {
	a := newTestAgent(t)
	before := a.Workspace()
	oldID := a.SessionID()
	a.history = []Message{{Role: "user", Content: "干活"}}
	a.handoffPend = &pendingHandoff{summary: "摘要", cont: true, workspace: filepath.Join(t.TempDir(), "gone")}
	a.applyHandoff()

	h, ok := a.TakeHandoff()
	if !ok || !h.Failed {
		t.Fatalf("应产生失败的交接快照: %+v ok=%v", h, ok)
	}
	if h.Err == "" {
		t.Error("应记录失败原因")
	}
	if h.NewID != "" {
		t.Errorf("失败不应有新会话 id: %q", h.NewID)
	}
	if a.Workspace() != before {
		t.Errorf("工作区不应变: %q", a.Workspace())
	}
	if a.SessionID() != oldID {
		t.Errorf("会话不应轮转: %q vs %q", a.SessionID(), oldID)
	}
	got := a.History()
	if len(got) != 2 || got[1].Role != "user" || !strings.Contains(got[1].Content, "会话交接未成立") {
		t.Fatalf("应追加未成立事实行供后续回合可见: %+v", got)
	}
	old := readSessionMsgs(t, a.SessionFile())
	last := old[len(old)-1]
	if !strings.Contains(last.Content, "会话交接未成立") {
		t.Errorf("落盘末条应为未成立事实行: %+v", last)
	}
	for _, msg := range old {
		if strings.Contains(msg.Content, "会话已移交至") {
			t.Errorf("作废的交接不应写移交事实行: %+v", msg)
		}
	}
	if _, err := a.RequestHandoff("再来一次", true, ""); err != nil {
		t.Errorf("作废后应能重新交接: %v", err)
	}
}

func TestHandoffWorkspaceNoSave(t *testing.T) {
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	a, err := New(cfg, NoSave(true), WithSystemPrompt(testBasePrompt))
	if err != nil {
		t.Fatal(err)
	}
	dirB := t.TempDir()
	a.handoffPend = &pendingHandoff{summary: "摘要", cont: true, workspace: dirB}
	a.applyHandoff()

	h, ok := a.TakeHandoff()
	if !ok || h.Failed || !h.NoSave {
		t.Fatalf("不落盘下交接应正常: %+v ok=%v", h, ok)
	}
	if a.Workspace() != dirB {
		t.Errorf("工作区应切到目标: %q", a.Workspace())
	}
	if _, ok := a.SessionDir(); ok {
		t.Error("不落盘模式不应有会话目录")
	}
	if _, err := os.Stat(filepath.Join(dirB, ".tanya")); !os.IsNotExist(err) {
		t.Errorf("不落盘模式不应创建 .tanya: %v", err)
	}
	if got := a.History(); len(got) != 1 || !strings.Contains(got[0].Content, dirB) {
		t.Errorf("内存 history 应为交接消息: %+v", got)
	}
}

func TestHandoffWorkspaceLoadFailureAborts(t *testing.T) {
	dirB := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirB, ".tanya"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, ".tanya", "sessions"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "next_session", args: handoffArgsWs("去 B", dirB)}}},
		mockStep{content: "收尾"},
	)
	a := newHandoffAgent(t, m)
	before := a.Workspace()
	oldID := a.SessionID()
	oldFile := a.store.path()
	if err := a.Ask(context.Background(), "干活", nil); err != nil {
		t.Fatal(err)
	}

	h, ok := a.TakeHandoff()
	if !ok || !h.Failed {
		t.Fatalf("会话目录建不出来时整次交接应作废: %+v ok=%v", h, ok)
	}
	if h.NewID != "" || a.Workspace() != before {
		t.Errorf("失败不应切换或开新会话: %+v ws=%q", h, a.Workspace())
	}
	if a.SessionID() != oldID {
		t.Errorf("会话不应轮转: %q vs %q", a.SessionID(), oldID)
	}
	if got := a.History(); got[len(got)-1].Content == "" || !strings.Contains(got[len(got)-1].Content, "会话交接未成立") {
		t.Errorf("应留下未成立事实行: %+v", got)
	}
	old := readSessionMsgs(t, oldFile)
	for _, msg := range old {
		if strings.Contains(msg.Content, "会话已移交至") {
			t.Errorf("作废的交接不应写移交事实行: %+v", msg)
		}
	}
}
