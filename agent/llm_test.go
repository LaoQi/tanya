package agent

import (
	"context"
	"fmt"
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
	ids, err := NewClient(cfg).ListModels()
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
	_, err := NewClient(cfg).ListModels()
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("应返回错误: %v", err)
	}
}

func TestChatStreamContent(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "你好，世界"})
	c := NewClient(m.config())
	var sb strings.Builder
	msg, err := c.ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		func(s string) { sb.WriteString(s) })
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
	c := NewClient(m.config())
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
	c := NewClient(m.config())
	_, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("应返回 500 错误: %v", err)
	}
}

func TestChatStreamNoAPIKey(t *testing.T) {
	cfg := defaultConfig()
	cfg.APIKey = ""
	c := NewClient(cfg)
	_, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Errorf("应提示缺少 api_key: %v", err)
	}
}

func TestChatStreamUsage(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "ok", usage: &Usage{PromptTokens: 120, CompletionTokens: 5, TotalTokens: 125}})
	c := NewClient(m.config())
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
	c := NewClient(m.config())
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
	c := NewClient(m.config())
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
	if len(req.Tools) != 4 {
		t.Errorf("工具数: %d", len(req.Tools))
	}
	if req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Error("messages 顺序异常")
	}
}
