package agent

import (
	"context"
	"encoding/json"
	"testing"
)

type stubTool struct {
	name string
	meta any
}

func (s *stubTool) Name() string { return s.name }

func (s *stubTool) Definition() ToolDef {
	return NewToolDef(s.name, "测试桩工具", `{"type":"object","properties":{"v":{"type":"string"}}}`)
}

func (s *stubTool) Invoke(_ context.Context, argsJSON string) ToolResult {
	return ToolResult{Text: s.name + ":" + argsJSON, Meta: s.meta}
}

type stubInteractiveTool struct{ stubTool }

func (s *stubInteractiveTool) Interactive(argsJSON string) bool {
	var args struct {
		Interactive bool `json:"interactive"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return false
	}
	return args.Interactive
}

func newStubTool(name string) *stubTool { return &stubTool{name: name} }

func testToolDefs() []ToolDef {
	r := newToolRegistry(assembleTools([]Tool{newStubTool("t1")}, &stubConfigTarget{})...)
	return r.defs()
}

func TestAssembleToolsOrderAndDefs(t *testing.T) {
	r := newToolRegistry(assembleTools([]Tool{newStubTool("t1"), newStubTool("t2")}, &stubConfigTarget{})...)
	defs := r.defs()
	want := []string{"t1", "t2", "agent_custom"}
	if len(defs) != len(want) {
		t.Fatalf("工具数 = %d, 期望 %d", len(defs), len(want))
	}
	for i, name := range want {
		if defs[i].Function.Name != name {
			t.Errorf("defs[%d].Name = %q, 期望 %q", i, defs[i].Function.Name, name)
		}
		if defs[i].Type != "function" {
			t.Errorf("defs[%d].Type = %q, 期望 function", i, defs[i].Type)
		}
		if defs[i].Function.Description == "" {
			t.Errorf("defs[%d] 描述为空", i)
		}
		if len(defs[i].Function.Parameters) == 0 {
			t.Errorf("defs[%d] 参数为空", i)
		}
	}
}

func TestAssembleToolsEmptyKeepsBuiltin(t *testing.T) {
	got := names(newToolRegistry(assembleTools(nil, &stubConfigTarget{})...).defs())
	if len(got) != 1 || got[0] != "agent_custom" {
		t.Fatalf("无注入时清单应只剩 agent_custom: %v", got)
	}
}

func TestToolRegistryLookup(t *testing.T) {
	stub := newStubTool("t1")
	r := newToolRegistry(assembleTools([]Tool{stub}, &stubConfigTarget{})...)
	tool, ok := r.lookup("t1")
	if !ok || tool != Tool(stub) {
		t.Fatalf("t1 应命中同一实例: ok=%v tool=%v", ok, tool)
	}
	if _, ok := r.lookup("no_such_tool"); ok {
		t.Error("未知工具不应命中")
	}
}

func TestInteractiveOf(t *testing.T) {
	it := &stubInteractiveTool{stubTool{name: "it"}}
	if !interactiveOf(it, `{"v":"x","interactive":true}`) {
		t.Error("interactive:true 应返回 true")
	}
	if interactiveOf(it, `{"v":"x"}`) {
		t.Error("缺省 interactive 应为 false")
	}
	if interactiveOf(it, `{bad`) {
		t.Error("坏 JSON 应为 false")
	}
	if interactiveOf(newStubTool("plain"), `{"v":"x","interactive":true}`) {
		t.Error("未实现 Interactive 的工具应恒 false")
	}
}

func TestWithToolsRegistered(t *testing.T) {
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	extra := NewTool("echo_tool", "回显入参", `{"type":"object","properties":{"v":{"type":"string"}}}`,
		func(_ context.Context, argsJSON string) ToolResult {
			return ToolResult{Text: "echo:" + argsJSON}
		})
	a, err := New(cfg, WithTools(extra))
	if err != nil {
		t.Fatal(err)
	}
	defs := a.tools.defs()
	if defs[len(defs)-1].Function.Name != "agent_custom" {
		t.Fatalf("agent_custom 应恒在清单末尾: %v", names(defs))
	}
	tool, ok := a.tools.lookup("echo_tool")
	if !ok {
		t.Fatal("注册工具应可被 lookup 命中")
	}
	if got := tool.Invoke(context.Background(), `{"v":"x"}`).Text; got != `echo:{"v":"x"}` {
		t.Errorf("注册工具执行异常: %q", got)
	}
	res := a.dispatch(context.Background(), "echo_tool", `{"v":"y"}`)
	if res.Text != `echo:{"v":"y"}` {
		t.Errorf("dispatch 注册工具异常: %q", res.Text)
	}
}

func TestWithToolsOrderBeforeBuiltin(t *testing.T) {
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	t1 := NewTool("t1", "d1", `{"type":"object"}`, func(context.Context, string) ToolResult { return ToolResult{} })
	t2 := NewTool("t2", "d2", `{"type":"object"}`, func(context.Context, string) ToolResult { return ToolResult{} })
	a, err := New(cfg, WithTools(t1, t2))
	if err != nil {
		t.Fatal(err)
	}
	got := names(a.tools.defs())
	want := []string{"t1", "t2", "agent_custom"}
	if len(got) != len(want) {
		t.Fatalf("清单 = %v, 期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("清单顺序 = %v, 期望 %v", got, want)
		}
	}
}

func TestWithToolsNoRegistrationKeepsDefaults(t *testing.T) {
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := names(a.tools.defs())
	want := []string{"agent_custom"}
	if len(got) != len(want) {
		t.Fatalf("不注册时清单应只剩 agent_custom: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("不注册时清单顺序应不变: %v", got)
		}
	}
}

func names(defs []ToolDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Function.Name
	}
	return out
}
