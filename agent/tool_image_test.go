package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type stubImageTool struct{}

func (s *stubImageTool) Name() string { return "read_image" }

func (s *stubImageTool) Definition() ToolDef {
	return NewToolDef(s.Name(), "测试桩：读图",
		`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}

func (s *stubImageTool) Invoke(context.Context, string) ToolResult {
	return ToolResult{
		Text: "已读取 a.png（2×2，1.0k）",
		Images: []ImageRef{{
			MIME: "image/png", Data: "AAAA", Name: "a.png", Bytes: 1024, Width: 2, Height: 2,
		}},
	}
}

func TestToolImagesAppendUserMessageChat(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "read_image", args: `{"path":"a.png"}`}}},
		mockStep{content: "看到了"},
	)
	a := newAgent(t, m, WithTools(&stubImageTool{}))
	if err := a.Ask(context.Background(), "看图", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if len(a.history) != 5 {
		t.Fatalf("history 应为 user/assistant/tool/user(图)/assistant: %+v", a.history)
	}
	note := a.history[3]
	if note.Role != "user" || len(note.Images) != 1 || note.Images[0].Data != "AAAA" {
		t.Fatalf("工具图像应转成紧随的 user 消息: %+v", note)
	}
	if !strings.Contains(note.Content, "read_image") || !strings.Contains(note.Content, "a.png") {
		t.Errorf("说明文案应含工具名与文件名: %q", note.Content)
	}
	if strings.Contains(note.Content, "AAAA") {
		t.Errorf("说明文案不得内联 base64: %q", note.Content)
	}
	if len(m.reqs) != 2 {
		t.Fatalf("请求数: %d", len(m.reqs))
	}
	msgs := m.reqs[1].Messages
	toolIdx := -1
	for i, v := range msgs {
		if wireRole(t, v) == "tool" {
			toolIdx = i
			break
		}
	}
	if toolIdx < 0 {
		t.Fatalf("第二轮请求应含 tool 消息: %#v", msgs)
	}
	if toolIdx+1 >= len(msgs) {
		t.Fatalf("tool 之后应紧跟 user 图像消息: %#v", msgs)
	}
	after := wireMap(t, msgs[toolIdx+1])
	if after["role"] != "user" {
		t.Fatalf("tool 之后应为 user: %#v", after)
	}
	parts, ok := after["content"].([]any)
	if !ok {
		t.Fatalf("带图 user 消息应为 content 数组: %#v", after["content"])
	}
	found := false
	for _, p := range parts {
		part, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if part["type"] == "image_url" {
			found = true
		}
	}
	if !found {
		t.Errorf("user 消息应带 image_url part: %#v", parts)
	}
}

func TestToolImagesAppendUserMessageResponses(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "read_image", args: `{"path":"a.png"}`}}},
		mockStep{content: "看到了"},
	)
	cfg := m.config()
	cfg.ApiProtocol = "responses"
	a := newAgentWithCfg(t, cfg, WithTools(&stubImageTool{}))
	if err := a.Ask(context.Background(), "看图", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if len(m.rawReqs) != 2 {
		t.Fatalf("请求数: %d", len(m.rawReqs))
	}
	input, ok := m.rawReqs[1]["input"].([]any)
	if !ok {
		t.Fatalf("input 应为数组: %#v", m.rawReqs[1]["input"])
	}
	outIdx := -1
	for i, v := range input {
		item, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if item["type"] == "function_call_output" {
			outIdx = i
			break
		}
	}
	if outIdx < 0 {
		t.Fatalf("第二轮 input 应含 function_call_output: %#v", input)
	}
	if outIdx+1 >= len(input) {
		t.Fatalf("function_call_output 之后应紧跟 user 图像消息: %#v", input)
	}
	next, _ := input[outIdx+1].(map[string]any)
	if next["type"] != "message" || next["role"] != "user" {
		t.Fatalf("tool 输出之后应为 user 消息: %#v", next)
	}
	content, _ := next["content"].([]any)
	found := false
	for _, p := range content {
		part, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if part["type"] == "input_image" {
			found = true
			if part["image_url"] != "data:image/png;base64,AAAA" {
				t.Errorf("input_image 的 image_url 应为字符串 data URL: %#v", part)
			}
		}
	}
	if !found {
		t.Errorf("user 消息应带 input_image part: %#v", content)
	}
	b, _ := json.Marshal(a.history)
	if !strings.Contains(string(b), "AAAA") {
		t.Errorf("history 应保留图像数据: %s", b)
	}
}

func TestToolImagesAfterAllParallelToolMessages(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{
			{id: "c1", name: "read_image", args: `{"path":"a.png"}`},
			{id: "c2", name: "run_shell", args: `{"command":"true"}`},
		}},
		mockStep{content: "看到了"},
	)
	a := newAgent(t, m, WithTools(&stubImageTool{}))
	if err := a.Ask(context.Background(), "看图并跑命令", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if len(a.history) != 6 {
		t.Fatalf("history 应为 user/assistant/tool/tool/user(图)/assistant: %d 条", len(a.history))
	}
	if a.history[2].Role != "tool" || a.history[3].Role != "tool" {
		t.Fatalf("两条 tool 消息应相邻: %+v", a.history[2:4])
	}
	note := a.history[4]
	if note.Role != "user" || len(note.Images) != 1 {
		t.Fatalf("图像 user 消息应在全部 tool 消息之后: %+v", note)
	}
	if !strings.Contains(note.Content, "read_image") || !strings.Contains(note.Content, "a.png") {
		t.Errorf("说明文案应含工具名与文件名: %q", note.Content)
	}
	msgs := m.reqs[1].Messages
	imgIdx, lastTool := -1, -1
	for i, v := range msgs {
		switch wireRole(t, v) {
		case "tool":
			lastTool = i
		case "user":
			parts, ok := wireMap(t, v)["content"].([]any)
			if !ok {
				continue
			}
			for _, p := range parts {
				part, ok := p.(map[string]any)
				if ok && part["type"] == "image_url" {
					imgIdx = i
				}
			}
		}
	}
	if imgIdx < 0 || lastTool < 0 || imgIdx <= lastTool {
		t.Fatalf("图像 user 消息应在所有 tool 消息之后: img=%d lastTool=%d", imgIdx, lastTool)
	}
	if imgIdx != len(msgs)-1 {
		t.Fatalf("图像 user 消息应是本轮最后一条: img=%d len=%d", imgIdx, len(msgs))
	}
}
