package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"model-b"},{"id":"model-a"},{"id":""}]}`)
	}))
	defer srv.Close()
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	ids, err := NewClient(cfg, nil).ListModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "model-a" || ids[1] != "model-b" {
		t.Errorf("应为排序后非空列表: %v", ids)
	}
}

func TestListModelsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	_, err := NewClient(cfg, nil).ListModels()
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("应返回错误: %v", err)
	}
}

func TestChatStreamReasoningEffort(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	cfg.ApiProtocol = "chat"
	cfg.ReasoningEffort = "max"
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"reasoning_effort":"max"`) {
		t.Errorf("应发送 reasoning_effort: %s", raw)
	}

	raw = nil
	cfg.ReasoningEffort = ""
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "reasoning_effort") {
		t.Errorf("未设置时不应发送 reasoning_effort: %s", raw)
	}
}

func rawChatServer(t *testing.T, raw *[]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*raw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestChatReplaysReasoningContent(t *testing.T) {
	var raw []byte
	srv := rawChatServer(t, &raw)
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	cfg.ApiProtocol = "chat"
	history := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok", ReasoningItems: []ReasoningItem{{ID: "rs_1", Content: "想了一下"}}},
		{Role: "user", Content: "next"},
	}
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(), history, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "reasoning_items") {
		t.Errorf("chat 请求不应携带 reasoning_items: %s", raw)
	}
	if !strings.Contains(string(raw), `"reasoning_content":"想了一下"`) {
		t.Errorf("chat 请求应回传 reasoning_content: %s", raw)
	}
}

func TestChatOmitsAbsentReasoning(t *testing.T) {
	var raw []byte
	srv := rawChatServer(t, &raw)
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	cfg.ApiProtocol = "chat"
	history := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "next"},
	}
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(), history, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "reasoning_content") {
		t.Errorf("历史无思维链时不应发送 reasoning_content: %s", raw)
	}
}

func TestChatEffortOmitsTemperature(t *testing.T) {
	var raw []byte
	srv := rawChatServer(t, &raw)
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	cfg.ApiProtocol = "chat"
	cfg.ReasoningEffort = "high"
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"temperature"`) {
		t.Errorf("设置 reasoning_effort 时不应发送 temperature: %s", raw)
	}

	raw = nil
	cfg.ReasoningEffort = ""
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"temperature"`) {
		t.Errorf("未设置时应发送 temperature: %s", raw)
	}
}

func TestChatStreamContent(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "你好，世界"})
	c := NewClient(m.config(), nil)
	var sb strings.Builder
	msg, err := c.ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		EventSink(func(e Event) {
			if e.Kind == EventContent {
				sb.WriteString(e.Text)
			}
		}))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "你好，世界" {
		t.Errorf("content: %q", msg.Content)
	}
	if sb.String() != "你好，世界" {
		t.Errorf("onDelta 累积: %q", sb.String())
	}
	if len(msg.ToolCalls) != 0 {
		t.Error("不应有 tool_calls")
	}
}

func TestChatStreamToolCallsMerge(t *testing.T) {
	m := newMockLLM(t, mockStep{toolCalls: []mockToolCall{
		{id: "call_1", name: "run_shell", args: `{"command":"echo hi","timeout":30}`},
		{id: "call_2", name: "calc", args: `{"expression":"1+2"}`},
	}})
	c := NewClient(m.config(), nil)
	msg, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("tool_calls 数: %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Type != "function" || tc.Function.Name != "run_shell" {
		t.Errorf("第一个 tool_call 异常: %+v", tc)
	}
	if tc.Function.Arguments != `{"command":"echo hi","timeout":30}` {
		t.Errorf("arguments 合并失败: %q", tc.Function.Arguments)
	}
	if msg.ToolCalls[1].Function.Name != "calc" {
		t.Errorf("第二个 tool_call 顺序异常")
	}
}

func TestChatStreamHTTPError(t *testing.T) {
	m := newMockLLM(t, mockStep{status: 500})
	c := NewClient(m.config(), nil)
	_, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("应返回 500 错误: %v", err)
	}
}

func TestChatStreamNoAPIKey(t *testing.T) {
	cfg := defaultConfig()
	cfg.APIKey = ""
	c := NewClient(cfg, nil)
	_, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Errorf("应提示缺少 api_key: %v", err)
	}
}

func TestChatStreamUsage(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "ok", usage: &Usage{PromptTokens: 120, CompletionTokens: 5, TotalTokens: 125}})
	c := NewClient(m.config(), nil)
	msg, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Usage == nil {
		t.Fatal("未捕获 usage")
	}
	if msg.Usage.PromptTokens != 120 || msg.Usage.TotalTokens != 125 {
		t.Errorf("usage: %+v", msg.Usage)
	}
	if len(m.reqs) != 1 || m.reqs[0].StreamOptions == nil || !m.reqs[0].StreamOptions.IncludeUsage {
		t.Error("请求应带 stream_options.include_usage")
	}
}

func TestChatStreamNoUsage(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "ok"})
	c := NewClient(m.config(), nil)
	msg, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Usage != nil {
		t.Error("对端未返回时 usage 应为 nil")
	}
}

func TestChatStreamRequestFormat(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "ok"})
	c := NewClient(m.config(), testToolDefs())
	_, err := c.ChatStream(context.Background(), []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.reqs) != 1 {
		t.Fatalf("请求数: %d", len(m.reqs))
	}
	req := m.reqs[0]
	if !req.Stream {
		t.Error("应为流式请求")
	}
	if len(req.Tools) != len(testToolDefs()) {
		t.Errorf("工具数: %d", len(req.Tools))
	}
	if req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Error("messages 顺序异常")
	}
}

func TestChatStreamReasoningEvents(t *testing.T) {
	m := newMockLLM(t, mockStep{reasoning: "先想一想", content: "答案是 42"})
	c := NewClient(m.config(), nil)
	var kinds []EventKind
	var reasoning, content strings.Builder
	sink := EventSink(func(e Event) {
		kinds = append(kinds, e.Kind)
		switch e.Kind {
		case EventReasoning:
			reasoning.WriteString(e.Text)
		case EventContent:
			content.WriteString(e.Text)
		}
	})
	msg, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if reasoning.String() != "先想一想" {
		t.Errorf("思维链事件: %q", reasoning.String())
	}
	if content.String() != "答案是 42" {
		t.Errorf("正文事件: %q", content.String())
	}
	if msg.Content != "答案是 42" {
		t.Errorf("思维链不应污染正文: %q", msg.Content)
	}
	if len(msg.ReasoningItems) != 1 || msg.ReasoningItems[0].Content != "先想一想" || msg.ReasoningItems[0].ID != "" {
		t.Errorf("chat 协议应累积思维链到 Message: %+v", msg.ReasoningItems)
	}
	if len(kinds) == 0 || kinds[0] != EventReasoning {
		t.Errorf("思维链事件应先于正文: %v", kinds)
	}
}

func TestChatStreamTimingDimensions(t *testing.T) {
	m := newMockLLM(t, mockStep{reasoning: "想", content: "答"})
	c := NewClient(m.config(), nil)
	msg, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := msg.Stat
	if st == nil {
		t.Fatal("应记录 Stat")
	}
	if st.FirstEvent <= 0 {
		t.Errorf("FirstEvent 应 >0: %v", st.FirstEvent)
	}
	if st.FirstReasoning <= 0 {
		t.Errorf("FirstReasoning 应 >0: %v", st.FirstReasoning)
	}
	if st.FirstContent < st.FirstReasoning {
		t.Errorf("FirstContent 不应早于 FirstReasoning: %v < %v", st.FirstContent, st.FirstReasoning)
	}
}

func TestChatStreamReasoningTokens(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "ok", usage: &Usage{
		PromptTokens:            120,
		CompletionTokens:        50,
		TotalTokens:             170,
		CompletionTokensDetails: &completionTokensDetails{ReasoningTokens: 42},
	}})
	c := NewClient(m.config(), nil)
	msg, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Usage == nil || msg.Usage.ReasoningTokens != 42 {
		t.Errorf("completion_tokens_details 应映射到 Usage.ReasoningTokens: %+v", msg.Usage)
	}
}
