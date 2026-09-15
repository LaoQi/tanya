package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type stubConfigTarget struct {
	model  string
	effort string
	models []string
	stats  Stats
	err    error
}

func (s *stubConfigTarget) Model() string                     { return s.model }
func (s *stubConfigTarget) SetModel(m string) error           { s.model = m; return nil }
func (s *stubConfigTarget) ReasoningEffort() string           { return s.effort }
func (s *stubConfigTarget) SetReasoningEffort(v string) error { s.effort = v; return nil }
func (s *stubConfigTarget) ListModels() ([]string, error)     { return s.models, s.err }
func (s *stubConfigTarget) Stats() Stats                      { return s.stats }

func newControlAgent(t *testing.T, m *mockLLM, protocol string) *Agent {
	t.Helper()
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.GlobalSession = t.TempDir()
	cfg.ApiProtocol = protocol
	cfg.Model = "old-model"
	if m != nil {
		cfg.BaseURL = m.server.URL
		cfg.APIKey = "test-key"
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func invokeAgentTool(t *testing.T, target configTarget, args string) string {
	t.Helper()
	return newAgentTool(target).Invoke(context.Background(), args).Text
}

func lastToolResult(t *testing.T, a *Agent) string {
	t.Helper()
	h := a.History()
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Role == "tool" {
			return h[i].Content
		}
	}
	t.Fatal("history 中没有工具结果")
	return ""
}

func TestAgentToolDescGolden(t *testing.T) {
	want := "读取或修改当前 agent 的模型与思考等级，并查询运行态统计与可用模型。" +
		"改动仅本次会话有效（不写入配置文件，进程退出即恢复），对下一次请求生效。" +
		"切换模型后 prompt cache 不复用，需重新预热。" +
		"action=get 读取现状；set 修改（至少指定 model 或 reasoning_effort 之一）；list_models 查询服务端可用模型（网络请求，最长 10s）。"
	tool := newAgentTool(&stubConfigTarget{})
	if got := tool.Definition().Function.Description; got != want {
		t.Errorf("描述全串不匹配:\n got %q\nwant %q", got, want)
	}
	if got := tool.Name(); got != "agent_custom" {
		t.Errorf("工具名 = %q, 期望 agent_custom", got)
	}
}

func TestAgentToolParamsGolden(t *testing.T) {
	want := `{"type":"object","properties":{` +
		`"action":{"type":"string","enum":["get","set","list_models"],"description":"get 读取当前可调项与运行态；set 修改；list_models 查询服务端可用模型（网络请求，最长 10s，需 api_key）"},` +
		`"model":{"type":"string","description":"set 时指定新模型名，缺省表示不改此项"},` +
		`"reasoning_effort":{"type":"string","enum":["minimal","low","medium","high","max","off"],"description":"set 时指定思考等级，off 表示不发送该字段"}},` +
		`"required":["action"]}`
	got := string(newAgentTool(&stubConfigTarget{}).Definition().Function.Parameters)
	if got != want {
		t.Errorf("参数全串不匹配:\n got %q\nwant %q", got, want)
	}
}

func TestAgentToolGet(t *testing.T) {
	a := newTestAgent(t)
	a.cfg.Model = "model-x"
	a.cfg.ReasoningEffort = "high"
	a.stats.record(&Usage{PromptTokens: 1000, CompletionTokens: 10, TotalTokens: 1010, CacheHitTokens: 750})
	want := "model: model-x\nreasoning_effort: high\n上下文: 1000 tokens（缓存命中 750，75.00%）\n消息数: 0"
	if got := invokeAgentTool(t, a, `{"action":"get"}`); got != want {
		t.Errorf("get 输出不匹配:\n got %q\nwant %q", got, want)
	}
}

func TestAgentToolGetFresh(t *testing.T) {
	a := newTestAgent(t)
	want := "model: deepseek-v4-flash\nreasoning_effort: (未设置)\n上下文: 未知（本轮尚无请求）\n消息数: 0"
	if got := invokeAgentTool(t, a, `{"action":"get"}`); got != want {
		t.Errorf("首轮 get 输出不匹配:\n got %q\nwant %q", got, want)
	}
}

func TestAgentToolSetModelAppliesNextRequest(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "agent_custom", args: `{"action":"set","model":"new-model"}`}}},
		mockStep{content: "done"},
	)
	a := newControlAgent(t, m, "chat")
	if err := a.Ask(context.Background(), "q", nil); err != nil {
		t.Fatal(err)
	}
	if len(m.reqs) != 2 {
		t.Fatalf("请求数 = %d, 期望 2", len(m.reqs))
	}
	if m.reqs[0].Model != "old-model" || m.reqs[1].Model != "new-model" {
		t.Errorf("模型未在下一请求生效: %q → %q", m.reqs[0].Model, m.reqs[1].Model)
	}
	if got := lastToolResult(t, a); !strings.Contains(got, "model: old-model → new-model") ||
		!strings.Contains(got, MsgControlCacheHint) {
		t.Errorf("set 返回文本异常: %q", got)
	}
}

func TestAgentToolSetModelResponses(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "agent_custom", args: `{"action":"set","model":"new-model","reasoning_effort":"low"}`}}},
		mockStep{content: "done"},
	)
	a := newControlAgent(t, m, "responses")
	if err := a.Ask(context.Background(), "q", nil); err != nil {
		t.Fatal(err)
	}
	if len(m.rawReqs) != 2 {
		t.Fatalf("请求数 = %d, 期望 2", len(m.rawReqs))
	}
	if m.rawReqs[0]["model"] != "old-model" || m.rawReqs[1]["model"] != "new-model" {
		t.Errorf("模型未在下一请求生效: %v → %v", m.rawReqs[0]["model"], m.rawReqs[1]["model"])
	}
	reasoning, ok := m.rawReqs[1]["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "low" {
		t.Errorf("reasoning.effort 未生效: %v", m.rawReqs[1]["reasoning"])
	}
	if _, ok := m.rawReqs[1]["temperature"]; ok {
		t.Error("设置 effort 后不应发送 temperature")
	}
}

func TestAgentToolSetEffortAppliesNextRequest(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "agent_custom", args: `{"action":"set","reasoning_effort":"high"}`}}},
		mockStep{content: "done"},
	)
	a := newControlAgent(t, m, "chat")
	if err := a.Ask(context.Background(), "q", nil); err != nil {
		t.Fatal(err)
	}
	if len(m.reqs) != 2 {
		t.Fatalf("请求数 = %d, 期望 2", len(m.reqs))
	}
	if m.reqs[0].ReasoningEffort != "" || m.reqs[0].Temperature == nil {
		t.Errorf("设置前应发送 temperature 且无 effort: %+v", m.reqs[0])
	}
	if m.reqs[1].ReasoningEffort != "high" || m.reqs[1].Temperature != nil {
		t.Errorf("effort 未生效或 temperature 未停发: %+v", m.reqs[1])
	}
}

func TestAgentToolSetEffortOff(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "agent_custom", args: `{"action":"set","reasoning_effort":"off"}`}}},
		mockStep{content: "done"},
	)
	a := newControlAgent(t, m, "chat")
	a.cfg.ReasoningEffort = "high"
	if err := a.Ask(context.Background(), "q", nil); err != nil {
		t.Fatal(err)
	}
	if a.ReasoningEffort() != "" {
		t.Errorf("off 应清空思考等级: %q", a.ReasoningEffort())
	}
	if m.reqs[1].ReasoningEffort != "" {
		t.Errorf("off 后不应发送 reasoning_effort: %+v", m.reqs[1])
	}
	if got := lastToolResult(t, a); !strings.Contains(got, "reasoning_effort: high → (未设置)") {
		t.Errorf("set 返回文本异常: %q", got)
	}
}

func TestAgentToolSetAtomic(t *testing.T) {
	a := newTestAgent(t)
	orig := a.Model()
	got := invokeAgentTool(t, a, `{"action":"set","model":"new-model","reasoning_effort":"bogus"}`)
	if a.Model() != orig {
		t.Errorf("非法 effort 不应使 model 生效: %q", a.Model())
	}
	if !strings.HasPrefix(got, MsgErrPrefix) || !strings.Contains(got, "无效思考等级") {
		t.Errorf("错误文本异常: %q", got)
	}
}

func TestAgentToolSetErrors(t *testing.T) {
	a := newTestAgent(t)
	cases := []struct {
		name string
		args string
		want string
	}{
		{"无改动", `{"action":"set"}`, MsgControlNoChange},
		{"空模型", `{"action":"set","model":"   "}`, MsgErrPrefix + MsgEmptyModel},
		{"未知 action", `{"action":"bogus"}`, fmt.Sprintf(MsgErrPrefix+MsgControlBadAction, "bogus")},
		{"缺 action", `{}`, fmt.Sprintf(MsgErrPrefix+MsgControlBadAction, "")},
		{"坏 JSON", `{`, MsgErrPrefix},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := invokeAgentTool(t, a, c.args)
			if !strings.HasPrefix(got, c.want) {
				t.Errorf("错误文本 = %q, 期望前缀 %q", got, c.want)
			}
		})
	}
	if a.Model() != "deepseek-v4-flash" {
		t.Errorf("错误分支不应改动模型: %q", a.Model())
	}
}

func TestAgentToolListModels(t *testing.T) {
	m := newMockLLM(t)
	a := newControlAgent(t, m, "chat")
	want := "可用模型（2）:\n  model-a\n  model-b"
	if got := invokeAgentTool(t, a, `{"action":"list_models"}`); got != want {
		t.Errorf("模型列表不匹配:\n got %q\nwant %q", got, want)
	}
}

func TestAgentToolListModelsTrim(t *testing.T) {
	m := newMockLLM(t)
	m.models = make([]string, 60)
	for i := range m.models {
		m.models[i] = fmt.Sprintf("model-%02d", i)
	}
	a := newControlAgent(t, m, "chat")
	got := invokeAgentTool(t, a, `{"action":"list_models"}`)
	lines := strings.Split(got, "\n")
	if lines[0] != "可用模型（60）:" || len(lines) != 52 {
		t.Errorf("截断输出异常（共 %d 行）: %q", len(lines), got)
	}
	if lines[len(lines)-1] != "（仅列出前 50 项，共 60 项）" {
		t.Errorf("截断提示异常: %q", lines[len(lines)-1])
	}
}

func TestAgentToolListModelsEmptyAndNoKey(t *testing.T) {
	m := newMockLLM(t)
	m.models = []string{}
	a := newControlAgent(t, m, "chat")
	if got := invokeAgentTool(t, a, `{"action":"list_models"}`); got != MsgControlModelsEmpty {
		t.Errorf("空列表输出 = %q", got)
	}
	b := newTestAgent(t)
	if got := invokeAgentTool(t, b, `{"action":"list_models"}`); !strings.HasPrefix(got, MsgErrPrefix) {
		t.Errorf("无 api_key 应报错: %q", got)
	}
}
