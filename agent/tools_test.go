package agent

import (
	"context"
	"strings"
	"testing"
)

func shellOf(t *testing.T, a *Agent) *shellTool {
	t.Helper()
	tool, ok := a.tools.lookup("run_shell")
	if !ok {
		t.Fatal("run_shell 未在注册表中")
	}
	st, ok := tool.(*shellTool)
	if !ok {
		t.Fatalf("run_shell 不是 *shellTool: %T", tool)
	}
	return st
}

func newTestShellTool() *shellTool {
	return &shellTool{
		profile:  &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix},
		programs: []string{"ls"},
	}
}

func TestToolRegistryOrderAndDefs(t *testing.T) {
	r := newToolRegistry(allTools(newTestShellTool(), &stubConfigTarget{})...)
	defs := r.defs()
	want := []string{"run_shell", "get_time", "get_env", "calc", "agent_custom"}
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

func TestToolRegistryLookup(t *testing.T) {
	shell := newTestShellTool()
	r := newToolRegistry(allTools(shell, &stubConfigTarget{})...)
	tool, ok := r.lookup("run_shell")
	if !ok || tool != shell {
		t.Fatalf("run_shell 应命中同一 shellTool 实例: ok=%v tool=%v", ok, tool)
	}
	if _, ok := r.lookup("no_such_tool"); ok {
		t.Error("未知工具不应命中")
	}
}

func TestInteractiveOf(t *testing.T) {
	shell := newTestShellTool()
	if !interactiveOf(shell, `{"command":"x","interactive":true}`) {
		t.Error("interactive:true 应返回 true")
	}
	if interactiveOf(shell, `{"command":"x"}`) {
		t.Error("缺省 interactive 应为 false")
	}
	if interactiveOf(shell, `{bad`) {
		t.Error("坏 JSON 应为 false")
	}
	for _, tool := range builtinTools() {
		if interactiveOf(tool, `{}`) {
			t.Errorf("%s 不应实现 Interactive", tool.Name())
		}
	}
}

func TestShellToolInvokeBadArgs(t *testing.T) {
	res := newTestShellTool().Invoke(context.Background(), `{bad`)
	if res.Meta != nil {
		t.Fatal("坏参数不应执行命令")
	}
	if !strings.Contains(res.Text, "参数解析失败") {
		t.Fatalf("坏参数应返回解析错误: %q", res.Text)
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
	if defs[len(defs)-1].Function.Name != "echo_tool" {
		t.Fatalf("注册工具应在清单末尾: %v", names(defs))
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

func TestWithToolsOrderAfterBuiltin(t *testing.T) {
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
	want := []string{"run_shell", "get_time", "get_env", "calc", "agent_custom", "t1", "t2"}
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
	want := []string{"run_shell", "get_time", "get_env", "calc", "agent_custom"}
	if len(got) != len(want) {
		t.Fatalf("不注册时清单应不变: %v", got)
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
