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
			t.Errorf("%s 不应实现 interactiveTool", tool.Name())
		}
	}
}

func TestShellToolInvokeBadArgs(t *testing.T) {
	res := newTestShellTool().Invoke(context.Background(), `{bad`)
	if res.Shell != nil {
		t.Fatal("坏参数不应执行命令")
	}
	if !strings.Contains(res.Text, "参数解析失败") {
		t.Fatalf("坏参数应返回解析错误: %q", res.Text)
	}
}
