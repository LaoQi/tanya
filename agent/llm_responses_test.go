package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func responsesLLM(t *testing.T, steps ...mockStep) (*mockLLM, *Config) {
	t.Helper()
	m := newMockLLM(t, steps...)
	cfg := m.config()
	cfg.ApiProtocol = "responses"
	return m, cfg
}

func TestResponsesContentAndUsage(t *testing.T) {
	m, cfg := responsesLLM(t, mockStep{
		content:   "你好，世界",
		reasoning: "enc-abc",
		usage: &Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
			ReasoningTokens:  30,
		},
	})
	cfg.ReasoningEffort = "high"
	var sb strings.Builder
	msg, err := NewClient(cfg, testToolDefs()).ChatStream(context.Background(),
		[]Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}},
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
	if len(msg.ReasoningItems) != 1 {
		t.Fatalf("reasoning items 数: %d", len(msg.ReasoningItems))
	}
	if msg.ReasoningItems[0].ID != "rs_test" || msg.ReasoningItems[0].Content != "enc-abc" {
		t.Errorf("reasoning item: %+v", msg.ReasoningItems[0])
	}
	if msg.Usage == nil || msg.Usage.PromptTokens != 100 || msg.Usage.TotalTokens != 150 {
		t.Fatalf("usage: %+v", msg.Usage)
	}
	if msg.Usage.CacheHit() != 0 {
		t.Errorf("无缓存时 CacheHit 应为 0: %d", msg.Usage.CacheHit())
	}
	if msg.Usage.ReasoningTokens != 30 {
		t.Errorf("reasoning tokens: %d", msg.Usage.ReasoningTokens)
	}

	req := m.rawReqs[0]
	if v, ok := req["store"].(bool); !ok || v {
		t.Errorf("store 应为 false: %v", req["store"])
	}
	if v, ok := req["instructions"].(string); !ok || v != "sys" {
		t.Errorf("instructions: %v", req["instructions"])
	}
	if v, ok := req["reasoning"].(map[string]any); !ok || v["effort"] != "high" {
		t.Errorf("reasoning.effort: %v", req["reasoning"])
	}
	if _, ok := req["include"]; ok {
		t.Errorf("请求不应携带 include 字段: %v", req["include"])
	}
	input, _ := req["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input 数: %d", len(input))
	}
	first := input[0].(map[string]any)
	if first["type"] != "message" || first["role"] != "user" {
		t.Errorf("input[0]: %v", first)
	}
	tools, _ := req["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("工具数: %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "run_shell" {
		t.Errorf("扁平工具格式: %v", tool)
	}
	if _, nested := tool["function"]; nested {
		t.Error("工具不应有嵌套 function 字段")
	}
}

func TestResponsesToolLoopReasoningReplay(t *testing.T) {
	m, cfg := responsesLLM(t,
		mockStep{
			reasoning: "enc-1",
			toolCalls: []mockToolCall{{id: "call_1", name: "calc", args: `{"expression":"1+2"}`}},
		},
		mockStep{content: "结果是 3"},
	)
	c := NewClient(cfg, nil)
	ctx := context.Background()

	first, err := c.ChatStream(ctx, []Message{{Role: "user", Content: "算 1+2"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 {
		t.Fatalf("tool_calls 数: %d", len(first.ToolCalls))
	}
	if first.ToolCalls[0].ID != "call_1" || first.ToolCalls[0].Function.Arguments != `{"expression":"1+2"}` {
		t.Errorf("tool_call: %+v", first.ToolCalls[0])
	}

	history := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "算 1+2"},
		*first,
		{Role: "tool", ToolCallID: "call_1", Name: "calc", Content: "3"},
	}
	second, err := c.ChatStream(ctx, history, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Content != "结果是 3" {
		t.Errorf("second content: %q", second.Content)
	}

	if len(m.rawReqs) != 2 {
		t.Fatalf("请求数: %d", len(m.rawReqs))
	}
	input, _ := m.rawReqs[1]["input"].([]any)
	var kinds []string
	for _, it := range input {
		kinds = append(kinds, it.(map[string]any)["type"].(string))
	}
	want := []string{"message", "reasoning", "function_call", "function_call_output"}
	if len(kinds) != len(want) {
		t.Fatalf("input items: %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("input 顺序: got %v want %v", kinds, want)
		}
	}
	rs := input[1].(map[string]any)
	if rs["id"] != "rs_test" {
		t.Errorf("reasoning 回传: %v", rs)
	}
	content, _ := rs["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("reasoning content parts: %v", content)
	}
	part := content[0].(map[string]any)
	if part["type"] != "reasoning_text" || part["text"] != "enc-1" {
		t.Errorf("reasoning 明文回传: %v", part)
	}
	if _, ok := rs["encrypted_content"]; ok {
		t.Error("reasoning 回传不应携带 encrypted_content")
	}
	fc := input[2].(map[string]any)
	if fc["call_id"] != "call_1" || fc["name"] != "calc" {
		t.Errorf("function_call 回传: %v", fc)
	}
	out := input[3].(map[string]any)
	if out["call_id"] != "call_1" || out["output"] != "3" {
		t.Errorf("function_call_output 回传: %v", out)
	}
}

func TestResponsesEffortOmitsTemperature(t *testing.T) {
	m, cfg := responsesLLM(t, mockStep{content: "ok"}, mockStep{content: "ok"})
	cfg.ReasoningEffort = "high"
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.rawReqs[0]["temperature"]; ok {
		t.Errorf("设置 reasoning_effort 时不应发送 temperature: %v", m.rawReqs[0])
	}

	cfg.ReasoningEffort = ""
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.rawReqs[1]["temperature"]; !ok {
		t.Errorf("未设置时应发送 temperature: %v", m.rawReqs[1])
	}
}

func TestResponsesTTFTToolCallOnly(t *testing.T) {
	_, cfg := responsesLLM(t, mockStep{
		toolCalls: []mockToolCall{{id: "call_1", name: "calc", args: `{"expression":"1+2"}`}},
	})
	msg, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "" || len(msg.ToolCalls) != 1 {
		t.Fatalf("应为纯 tool_call 响应: %+v", msg)
	}
	if msg.Stat == nil || msg.Stat.FirstEvent <= 0 {
		t.Errorf("纯 tool_call 响应也应记录 FirstEvent: %+v", msg.Stat)
	}
}

func TestResponsesCompletedMultiMessageItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"甲"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"乙"}]}]}}`+"\n\n")
	}))
	defer srv.Close()
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	msg, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "甲乙" {
		t.Errorf("多 message item 应拼接: %q", msg.Content)
	}
}

func TestResponsesCompletedSkipsMessageWhenDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"流式"}`+"\n\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"终态"},{"type":"output_text","text":"拼接"}]}]}}`+"\n\n")
	}))
	defer srv.Close()
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	var sb strings.Builder
	msg, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, EventSink(func(e Event) {
			if e.Kind == EventContent {
				sb.WriteString(e.Text)
			}
		}))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "流式" {
		t.Errorf("收到 delta 后应跳过 completed 的 message 拼接: %q", msg.Content)
	}
	if sb.String() != "流式" {
		t.Errorf("onDelta 累积: %q", sb.String())
	}
}

func TestResponsesNoEffortNoReasoningField(t *testing.T) {
	m, cfg := responsesLLM(t, mockStep{content: "ok"})
	cfg.ReasoningEffort = ""
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.rawReqs[0]["reasoning"]; ok {
		t.Error("未设置 effort 时不应携带 reasoning 字段")
	}
}

func TestResponsesNoReasoningInReplayWhenAbsent(t *testing.T) {
	m, cfg := responsesLLM(t, mockStep{content: "ok"})
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	isolatePromptEnv(t)
	a.history = []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok"},
	}
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(), a.buildMessages(), nil); err != nil {
		t.Fatal(err)
	}
	input, _ := m.rawReqs[0]["input"].([]any)
	for _, it := range input {
		if it.(map[string]any)["type"] == "reasoning" {
			t.Fatal("无 reasoning 历史不应回传 reasoning item")
		}
	}
}

func TestResponsesSkipEmptyContentReasoningInReplay(t *testing.T) {
	m, cfg := responsesLLM(t, mockStep{content: "ok"})
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	isolatePromptEnv(t)
	a.history = []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok", ReasoningItems: []ReasoningItem{
			{ID: "rs_1", Content: ""},
			{ID: "rs_2", Content: "有的推理"},
		}},
	}
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(), a.buildMessages(), nil); err != nil {
		t.Fatal(err)
	}
	input, _ := m.rawReqs[0]["input"].([]any)
	var reasoning []map[string]any
	for _, it := range input {
		if it.(map[string]any)["type"] == "reasoning" {
			reasoning = append(reasoning, it.(map[string]any))
		}
	}
	if len(reasoning) != 1 {
		t.Fatalf("应仅回传非空 content 的 reasoning: %d", len(reasoning))
	}
	if reasoning[0]["id"] != "rs_2" {
		t.Errorf("回传的应是 rs_2: %v", reasoning[0])
	}
}

func TestResponsesUsageCacheHit(t *testing.T) {
	u := usageFromResponses(&responsesUsage{
		InputTokens:  200,
		OutputTokens: 10,
		TotalTokens:  210,
		InputTokensDetails: &struct {
			CachedTokens int `json:"cached_tokens"`
		}{CachedTokens: 150},
	})
	if u.PromptTokens != 200 || u.CompletionTokens != 10 || u.TotalTokens != 210 {
		t.Errorf("usage 映射: %+v", u)
	}
	if u.CacheHit() != 150 {
		t.Errorf("cached_tokens 映射: %d", u.CacheHit())
	}
}

func TestResponsesFailedEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"x\",\"message\":\"boom\"}}}\n\n")
	}))
	defer srv.Close()
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	_, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("应返回 failed 事件错误: %v", err)
	}
}

func TestResponsesErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"code\":\"y\",\"message\":\"bad\"}}\n\n")
	}))
	defer srv.Close()
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	_, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Errorf("应返回 error 事件: %v", err)
	}
}

func TestResponses404Hint(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKey = "test-key"
	_, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), MsgRespHint404) {
		t.Errorf("404 应附带协议提示: %v", err)
	}
}

func TestResponsesAgentLoopSessionReasoning(t *testing.T) {
	m, cfg := responsesLLM(t,
		mockStep{
			reasoning: "enc-loop",
			toolCalls: []mockToolCall{{id: "call_9", name: "calc", args: `{"expression":"2*3"}`}},
		},
		mockStep{content: "6"},
	)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	isolatePromptEnv(t)
	a.NewSession()
	if err := a.Ask(context.Background(), "算 2*3", nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range readLines(t, a.store.path()) {
		var msg Message
		if json.Unmarshal([]byte(line), &msg) != nil {
			continue
		}
		if msg.Role == "assistant" && len(msg.ReasoningItems) > 0 {
			found = true
			if msg.ReasoningItems[0].Content != "enc-loop" {
				t.Errorf("会话中 reasoning: %+v", msg.ReasoningItems[0])
			}
		}
	}
	if !found {
		t.Error("会话文件应保存 reasoning_items")
	}
	if len(m.rawReqs) != 2 {
		t.Fatalf("请求数: %d", len(m.rawReqs))
	}
	input, _ := m.rawReqs[1]["input"].([]any)
	last := input[len(input)-1].(map[string]any)
	if last["type"] != "function_call_output" {
		t.Errorf("末位 item: %v", last)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestResponsesReasoningDeltaEvents(t *testing.T) {
	_, cfg := responsesLLM(t, mockStep{reasoning: "推理过程", content: "结论"})
	var reasoning, content strings.Builder
	sink := EventSink(func(e Event) {
		switch e.Kind {
		case EventReasoning:
			reasoning.WriteString(e.Text)
		case EventContent:
			content.WriteString(e.Text)
		}
	})
	msg, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if reasoning.String() != "推理过程" {
		t.Errorf("思维链事件: %q", reasoning.String())
	}
	if content.String() != "结论" {
		t.Errorf("正文事件: %q", content.String())
	}
	if msg.Content != "结论" {
		t.Errorf("思维链不应污染正文: %q", msg.Content)
	}
	if len(msg.ReasoningItems) != 1 || msg.ReasoningItems[0].Content != "推理过程" {
		t.Errorf("completed 仍应捕获思维链: %+v", msg.ReasoningItems)
	}
	if msg.Stat == nil || msg.Stat.FirstReasoning <= 0 || msg.Stat.FirstContent < msg.Stat.FirstReasoning {
		t.Errorf("时序维度异常: %+v", msg.Stat)
	}
}

func TestResponsesToolCallDeltaEvents(t *testing.T) {
	_, cfg := responsesLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "run_shell", args: `{"command":"echo hi"}`}}},
		mockStep{content: "done"},
	)
	var args strings.Builder
	sink := EventSink(func(e Event) {
		if e.Kind == EventToolCall {
			args.WriteString(e.ToolArgs)
		}
	})
	msg, err := NewClient(cfg, nil).ChatStream(context.Background(),
		[]Message{{Role: "user", Content: "hi"}}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if args.String() != `{"command":"echo hi"}` {
		t.Errorf("工具参数增量事件: %q", args.String())
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Arguments != `{"command":"echo hi"}` {
		t.Errorf("终态仍应从 completed 提取 tool_calls: %+v", msg.ToolCalls)
	}
}

func TestResponsesErrorKeepsPartialTurn(t *testing.T) {
	m, cfg := responsesLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "calc", args: `{"expression":"2*3"}`}}},
		mockStep{status: 500},
		mockStep{content: "继续"},
	)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "算 2*3", nil); err == nil {
		t.Fatal("第一轮应返回错误")
	}
	if len(a.history) != 4 {
		t.Fatalf("应保留 user+assistant+tool+错误提示 共4条: %d", len(a.history))
	}
	if last := a.history[3]; last.Role != "user" || !strings.HasPrefix(last.Content, "[本轮因错误中止") {
		t.Fatalf("末条应为错误提示: %+v", last)
	}
	if err := a.Ask(context.Background(), "继续", nil); err != nil {
		t.Fatal(err)
	}
	if len(m.rawReqs) != 3 {
		t.Fatalf("应有 3 次请求: %d", len(m.rawReqs))
	}
	input, _ := m.rawReqs[2]["input"].([]any)
	found := false
	for _, it := range input {
		item, _ := it.(map[string]any)
		if item["role"] != "user" {
			continue
		}
		parts, _ := item["content"].([]any)
		for _, p := range parts {
			part, _ := p.(map[string]any)
			if text, _ := part["text"].(string); strings.HasPrefix(text, "[本轮因错误中止") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("续接请求 input 应含错误提示: %+v", input)
	}
}

func TestResponsesOmitsEmptyReasoningID(t *testing.T) {
	m, cfg := responsesLLM(t, mockStep{content: "ok"})
	history := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok", ReasoningItems: []ReasoningItem{{Content: "chat 来的推理"}}},
	}
	if _, err := NewClient(cfg, nil).ChatStream(context.Background(), history, nil); err != nil {
		t.Fatal(err)
	}
	input, _ := m.rawReqs[0]["input"].([]any)
	var reasoning map[string]any
	for _, it := range input {
		if item, ok := it.(map[string]any); ok && item["type"] == "reasoning" {
			reasoning = item
		}
	}
	if reasoning == nil {
		t.Fatal("应回传 reasoning item")
	}
	if _, ok := reasoning["id"]; ok {
		t.Errorf("空 id 不应出现在请求里: %v", reasoning)
	}
}
