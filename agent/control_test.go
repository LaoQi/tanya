package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type stubConfigTarget struct {
	model    string
	effort   string
	models   []string
	sessions []SessionInfo
	stats    Stats
	noSave   bool
	err      error
	path     string
}

func (s *stubConfigTarget) Model() string                        { return s.model }
func (s *stubConfigTarget) SetModel(m string) error              { s.model = m; return nil }
func (s *stubConfigTarget) ReasoningEffort() string              { return s.effort }
func (s *stubConfigTarget) SetReasoningEffort(v string) error    { s.effort = v; return nil }
func (s *stubConfigTarget) ListModels() ([]string, error)        { return s.models, s.err }
func (s *stubConfigTarget) ListSessions() ([]SessionInfo, error) { return s.sessions, s.err }
func (s *stubConfigTarget) ConfigPath() string                   { return s.path }
func (s *stubConfigTarget) NoSave() bool                         { return s.noSave }
func (s *stubConfigTarget) Stats() Stats                         { return s.stats }

func newControlAgent(t *testing.T, m *mockLLM, protocol string) *Agent {
	t.Helper()
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
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

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestAgentToolDescGolden(t *testing.T) {
	want := "读取或修改当前 agent 的模型与思考等级，并查询运行态统计、可用模型与会话列表。" +
		"改动仅本次会话有效（不写入配置文件，进程退出即恢复），对下一次请求生效。" +
		"切换模型后 prompt cache 不复用，需重新预热。" +
		"action=get 按 key 读取；action=set 按 key 写入可写 key（需同时给 value）。" +
		"key 可用: model、reasoning_effort、models、usage、stat、sessions、config_path；可写 key: model、reasoning_effort。"
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
		`"action":{"type":"string","enum":["get","set"],"description":"get 读取 key 的当前值；set 写入可写 key（需同时给 value）"},` +
		`"key":{"type":"string","enum":["model","reasoning_effort","models","usage","stat","sessions","config_path"],"description":"可写键 model、reasoning_effort；只读键 models（服务端可用模型）、usage（上下文与缓存）、stat（会话统计）、sessions（会话列表与文件路径，jsonl 每行一条消息）、config_path（生效配置文件绝对路径，可用 run_shell 读取或修改，改动需重启生效）"},` +
		`"value":{"type":"string","description":"set 的新值（get 时忽略）。reasoning_effort 取 minimal/low/medium/high/max/off，off 表示清空该字段"}},` +
		`"required":["action","key"]}`
	got := string(newAgentTool(&stubConfigTarget{}).Definition().Function.Parameters)
	if got != want {
		t.Errorf("参数全串不匹配:\n got %q\nwant %q", got, want)
	}
}

func TestAgentToolKeyTableConsistent(t *testing.T) {
	specs := newAgentTool(&stubConfigTarget{}).keys()
	if len(specs) != len(agentKeyNames) {
		t.Fatalf("key 表 %d 项, agentKeyNames %d 项", len(specs), len(agentKeyNames))
	}
	for _, name := range agentKeyNames {
		spec, ok := specs[name]
		if !ok {
			t.Errorf("key 表缺少 %q", name)
			continue
		}
		if spec.read == nil {
			t.Errorf("%q 缺少 read", name)
		}
		writable := spec.writable != ""
		if writable != hasString(agentWritableKeys, name) {
			t.Errorf("%q 可写性不一致 (writable=%q)", name, spec.writable)
		}
		if writable && spec.write == nil {
			t.Errorf("%q 可写但缺少 write", name)
		}
	}
}

func TestAgentToolConfigPath(t *testing.T) {
	st := &stubConfigTarget{path: "/home/u/.config/tanya/config.yaml"}
	want := "配置文件: /home/u/.config/tanya/config.yaml"
	if got := invokeAgentTool(t, st, `{"action":"get","key":"config_path"}`); got != want {
		t.Errorf("config_path 输出不匹配:\n got %q\nwant %q", got, want)
	}
	got := invokeAgentTool(t, st, `{"action":"set","key":"config_path","value":"/tmp/other.yaml"}`)
	if !strings.Contains(got, "只读 key") || !strings.HasPrefix(got, MsgErrPrefix) {
		t.Errorf("set config_path 应明确拒绝: %q", got)
	}
	if got := invokeAgentTool(t, st, `{"action":"get","key":"config_paths"}`); !strings.Contains(got, "未知 key") {
		t.Errorf("未知 key 应报错: %q", got)
	}
}

func TestAgentConfigPathFollowsConfig(t *testing.T) {
	a := newControlAgent(t, nil, "responses")
	if got := a.ConfigPath(); got != a.cfg.ConfigPath || got == "" {
		t.Errorf("ConfigPath = %q, cfg.ConfigPath = %q", got, a.cfg.ConfigPath)
	}
}

func TestAgentToolGetScalars(t *testing.T) {
	cases := []struct {
		name string
		stub *stubConfigTarget
		key  string
		want string
	}{
		{"model", &stubConfigTarget{model: "model-x"}, "model", "model: model-x"},
		{"effort 已设置", &stubConfigTarget{effort: "high"}, "reasoning_effort", "reasoning_effort: high"},
		{"effort 未设置", &stubConfigTarget{}, "reasoning_effort", "reasoning_effort: (未设置)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := invokeAgentTool(t, c.stub, fmt.Sprintf(`{"action":"get","key":%q}`, c.key))
			if got != c.want {
				t.Errorf("get %s = %q, 期望 %q", c.key, got, c.want)
			}
		})
	}
}

func TestAgentToolGetUsage(t *testing.T) {
	st := &stubConfigTarget{stats: Stats{HasContext: true, ContextTokens: 1000, ContextHit: 750}}
	want := "上下文: 1000 tokens（最近一次请求）\n缓存命中: 750（75.00%）"
	if got := invokeAgentTool(t, st, `{"action":"get","key":"usage"}`); got != want {
		t.Errorf("usage 输出不匹配:\n got %q\nwant %q", got, want)
	}
	fresh := &stubConfigTarget{}
	if got := invokeAgentTool(t, fresh, `{"action":"get","key":"usage"}`); got != MsgControlContextUnknown {
		t.Errorf("首轮 usage 输出 = %q", got)
	}
}

func TestAgentToolGetStat(t *testing.T) {
	st := &stubConfigTarget{stats: Stats{
		Session:          "/tmp/s/20260915-145844.jsonl",
		Messages:         18,
		PromptTokens:     45678,
		CompletionTokens: 1234,
		TotalTokens:      46912,
	}}
	want := "会话: 20260915-145844\n消息数: 18\n累计: prompt 45678 / completion 1234 / total 46912"
	if got := invokeAgentTool(t, st, `{"action":"get","key":"stat"}`); got != want {
		t.Errorf("stat 输出不匹配:\n got %q\nwant %q", got, want)
	}
	noSave := &stubConfigTarget{noSave: true, stats: Stats{Session: "/tmp/s/20260915-145844.jsonl"}}
	wantNoSave := fmt.Sprintf(MsgControlStatSession, MsgControlStatNoSave)
	if got := invokeAgentTool(t, noSave, `{"action":"get","key":"stat"}`); !strings.HasPrefix(got, wantNoSave) {
		t.Errorf("不落盘时 stat 输出异常: %q", got)
	}
}

func TestAgentToolGetStatNoSave(t *testing.T) {
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.Model = "old-model"
	a, err := New(cfg, NoSave(true))
	if err != nil {
		t.Fatal(err)
	}
	if !a.NoSave() {
		t.Fatal("Agent 未进入不落盘模式")
	}
	got := invokeAgentTool(t, a, `{"action":"get","key":"stat"}`)
	if !strings.HasPrefix(got, fmt.Sprintf(MsgControlStatSession, MsgControlStatNoSave)) {
		t.Errorf("不落盘时 stat 输出异常: %q", got)
	}
}

func TestAgentToolGetModels(t *testing.T) {
	m := newMockLLM(t)
	a := newControlAgent(t, m, "chat")
	want := "可用模型（2）:\n  model-a\n  model-b"
	if got := invokeAgentTool(t, a, `{"action":"get","key":"models"}`); got != want {
		t.Errorf("模型列表不匹配:\n got %q\nwant %q", got, want)
	}

	m.models = make([]string, 60)
	for i := range m.models {
		m.models[i] = fmt.Sprintf("model-%02d", i)
	}
	got := invokeAgentTool(t, a, `{"action":"get","key":"models"}`)
	lines := strings.Split(got, "\n")
	if lines[0] != "可用模型（60）:" || len(lines) != 52 {
		t.Errorf("截断输出异常（共 %d 行）: %q", len(lines), got)
	}
	if lines[len(lines)-1] != "（仅列出前 50 项，共 60 项）" {
		t.Errorf("截断提示异常: %q", lines[len(lines)-1])
	}

	m.models = []string{}
	if got := invokeAgentTool(t, a, `{"action":"get","key":"models"}`); got != MsgControlModelsEmpty {
		t.Errorf("空列表输出 = %q", got)
	}

	b := newTestAgent(t)
	if got := invokeAgentTool(t, b, `{"action":"get","key":"models"}`); !strings.HasPrefix(got, MsgErrPrefix) {
		t.Errorf("无 api_key 应报错: %q", got)
	}
}

func TestAgentToolGetSessions(t *testing.T) {
	mt := time.Date(2026, 9, 15, 14, 58, 0, 0, time.UTC)
	st := &stubConfigTarget{sessions: []SessionInfo{
		{ID: "20260915-145844", ModTime: mt, Msgs: 18, Path: "/tmp/s/20260915-145844.jsonl"},
	}}
	want := "会话（1，最新在前）:\n  20260915-145844  18 条  2026-09-15 14:58  /tmp/s/20260915-145844.jsonl"
	if got := invokeAgentTool(t, st, `{"action":"get","key":"sessions"}`); got != want {
		t.Errorf("sessions 输出不匹配:\n got %q\nwant %q", got, want)
	}

	empty := &stubConfigTarget{}
	if got := invokeAgentTool(t, empty, `{"action":"get","key":"sessions"}`); got != MsgControlSessionsEmpty {
		t.Errorf("空会列表输出 = %q", got)
	}

	many := &stubConfigTarget{}
	for i := 0; i < 25; i++ {
		many.sessions = append(many.sessions, SessionInfo{
			ID: fmt.Sprintf("20260915-14%02d00", i), ModTime: mt, Msgs: i,
			Path: fmt.Sprintf("/tmp/s/%02d.jsonl", i),
		})
	}
	got := invokeAgentTool(t, many, `{"action":"get","key":"sessions"}`)
	lines := strings.Split(got, "\n")
	if lines[0] != "会话（25，最新在前）:" || len(lines) != 22 {
		t.Errorf("截断输出异常（共 %d 行）: %q", len(lines), got)
	}
	if lines[len(lines)-1] != "（共 25 个，仅列前 20 个）" {
		t.Errorf("截断提示异常: %q", lines[len(lines)-1])
	}
}

func TestAgentToolSetModelAppliesNextRequest(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "agent_custom", args: `{"action":"set","key":"model","value":"new-model"}`}}},
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
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "agent_custom", args: `{"action":"set","key":"model","value":"new-model"}`}}},
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
}

func TestAgentToolSetModelTrim(t *testing.T) {
	a := newTestAgent(t)
	got := invokeAgentTool(t, a, `{"action":"set","key":"model","value":"  spaced  "}`)
	if a.Model() != "spaced" {
		t.Errorf("模型未 trim: %q", a.Model())
	}
	if !strings.Contains(got, "→ spaced") {
		t.Errorf("返回文本未使用 trim 后的值: %q", got)
	}
}

func TestAgentToolSetEffortResponses(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "agent_custom", args: `{"action":"set","key":"reasoning_effort","value":"low"}`}}},
		mockStep{content: "done"},
	)
	a := newControlAgent(t, m, "responses")
	if err := a.Ask(context.Background(), "q", nil); err != nil {
		t.Fatal(err)
	}
	if len(m.rawReqs) != 2 {
		t.Fatalf("请求数 = %d, 期望 2", len(m.rawReqs))
	}
	reasoning, ok := m.rawReqs[1]["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "low" {
		t.Errorf("reasoning.effort 未生效: %v", m.rawReqs[1]["reasoning"])
	}
	if _, ok := m.rawReqs[1]["temperature"]; ok {
		t.Error("设置 effort 后不应发送 temperature")
	}
}

func TestAgentToolSetEffortNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"HIGH", "high"},
		{"  low  ", "low"},
		{"max", "max"},
	}
	for _, c := range cases {
		a := newTestAgent(t)
		invokeAgentTool(t, a, fmt.Sprintf(`{"action":"set","key":"reasoning_effort","value":%q}`, c.in))
		if a.ReasoningEffort() != c.want {
			t.Errorf("effort %q 归一为 %q, 期望 %q", c.in, a.ReasoningEffort(), c.want)
		}
	}
}

func TestAgentToolSetEffortOff(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "agent_custom", args: `{"action":"set","key":"reasoning_effort","value":"off"}`}}},
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

func TestAgentToolSetRejectsReadOnlyKeys(t *testing.T) {
	for _, key := range []string{"models", "usage", "stat", "sessions"} {
		t.Run(key, func(t *testing.T) {
			a := newTestAgent(t)
			orig := a.Model()
			got := invokeAgentTool(t, a, fmt.Sprintf(`{"action":"set","key":%q,"value":"x"}`, key))
			want := fmt.Sprintf(MsgErrPrefix+MsgControlReadOnlyKey, key, agentWritableLabel())
			if got != want {
				t.Errorf("只读 key 错误文本 = %q, 期望 %q", got, want)
			}
			if a.Model() != orig {
				t.Errorf("只读 key 分支不应改动状态: %q", a.Model())
			}
		})
	}
}

func TestAgentToolErrors(t *testing.T) {
	a := newTestAgent(t)
	cases := []struct {
		name string
		args string
		want string
	}{
		{"未知 action", `{"action":"bogus","key":"model"}`, fmt.Sprintf(MsgErrPrefix+MsgControlBadAction, "bogus")},
		{"缺 action", `{"key":"model"}`, fmt.Sprintf(MsgErrPrefix+MsgControlBadAction, "")},
		{"缺 key", `{"action":"get"}`, fmt.Sprintf(MsgErrPrefix+MsgControlMissingKey, agentKeysLabel())},
		{"未知 key", `{"action":"get","key":"bogus"}`, fmt.Sprintf(MsgErrPrefix+MsgControlBadKey, "bogus", agentKeysLabel())},
		{"缺 value", `{"action":"set","key":"model"}`, fmt.Sprintf(MsgErrPrefix+MsgControlNeedValue, MsgControlModelSpec)},
		{"空模型", `{"action":"set","key":"model","value":"   "}`, MsgErrPrefix + MsgEmptyModel},
		{"非法 effort", `{"action":"set","key":"reasoning_effort","value":"bogus"}`, MsgErrPrefix},
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
