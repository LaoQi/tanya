package agent

import (
	"context"
	"strings"
	"testing"
)

func TestAskSingleTurn(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "这是回答"})
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := a.Ask(context.Background(), "问题", func(s string) { sb.WriteString(s) }); err != nil {
		t.Fatal(err)
	}
	if sb.String() != "这是回答" {
		t.Errorf("输出: %q", sb.String())
	}
	if len(a.history) != 2 {
		t.Fatalf("history: %d", len(a.history))
	}
	if len(m.reqs) != 1 {
		t.Fatalf("请求数: %d", len(m.reqs))
	}
	if m.reqs[0].Messages[0].Role != "system" {
		t.Error("首条应为 system prompt")
	}
}

func TestAskShellToolLoop(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "run_shell", args: `{"command":"echo hello-tool"}`}}},
		mockStep{content: "执行完毕"},
	)
	cfg := m.config()
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var toolEvents []string
	var startEvents []string
	a.OnToolStart = func(name, args string) { startEvents = append(startEvents, name+":"+args) }
	a.OnToolEnd = func(name, args string, res ToolResult) { toolEvents = append(toolEvents, name) }

	if err := a.Ask(context.Background(), "测试 shell", nil); err != nil {
		t.Fatal(err)
	}
	if len(a.history) != 4 {
		t.Fatalf("history: %d", len(a.history))
	}
	toolMsg := a.history[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_1" || toolMsg.Name != "run_shell" {
		t.Errorf("tool 消息异常: %+v", toolMsg)
	}
	if !strings.Contains(toolMsg.Content, "hello-tool") {
		t.Errorf("tool 结果: %q", toolMsg.Content)
	}
	if len(toolEvents) != 1 || toolEvents[0] != "run_shell" {
		t.Errorf("OnToolEnd 回调: %v", toolEvents)
	}
	if len(startEvents) != 1 || !strings.Contains(startEvents[0], "hello-tool") {
		t.Errorf("OnToolStart 回调: %v", startEvents)
	}
	if len(m.reqs) != 2 {
		t.Fatalf("请求数: %d", len(m.reqs))
	}
	second := m.reqs[1].Messages
	if len(second) != 4 || second[3].Role != "tool" {
		t.Fatalf("第二次请求应回填 tool 结果: %+v", second)
	}
}

func TestAskBuiltinToolLoop(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "calc", args: `{"expression":"6*7"}`}}},
		mockStep{content: "42"},
	)
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "算一下", nil); err != nil {
		t.Fatal(err)
	}
	if a.history[2].Content != "42" {
		t.Errorf("calc 结果: %q", a.history[2].Content)
	}
}

func TestAskUsageFallback(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回答"})
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.PromptUsage(), "~") {
		t.Errorf("无 usage 时应为估算: %q", a.PromptUsage())
	}
	if err := a.Ask(context.Background(), "问题", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.PromptUsage(), "~") {
		t.Errorf("对端未返回 usage 时仍为估算: %q", a.PromptUsage())
	}
	if !strings.Contains(a.ContextInfo(), "本地估算") {
		t.Errorf("ContextInfo: %q", a.ContextInfo())
	}
}

func TestAskUsageReal(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回答", usage: &Usage{PromptTokens: 1500, CompletionTokens: 10, TotalTokens: 1510}})
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "问题", nil); err != nil {
		t.Fatal(err)
	}
	if a.PromptUsage() != "1.5k" {
		t.Errorf("应显示实报 prompt tokens: %q", a.PromptUsage())
	}
	if !strings.Contains(a.ContextInfo(), "API 实报") {
		t.Errorf("ContextInfo: %q", a.ContextInfo())
	}
}

func TestAskUnknownTool(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "hack", args: `{}`}}},
		mockStep{content: "end"},
	)
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "go", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.history[2].Content, "未知工具") {
		t.Errorf("got %q", a.history[2].Content)
	}
}
