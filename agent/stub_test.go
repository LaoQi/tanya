package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	stubInterruptRunning    = MsgErrPrefix + "已中断（进程已终止，输出可能不完整）"
	stubInterruptNotStarted = MsgErrPrefix + "已中断（命令未执行）"
)

type stubShellMeta struct {
	Command string
}

type stubShellTool struct{}

func (s *stubShellTool) Name() string { return "run_shell" }

func (s *stubShellTool) Definition() ToolDef {
	return NewToolDef(s.Name(), "测试桩：模拟 run_shell 的解析、中断与结构化结果",
		`{"type":"object","properties":{"command":{"type":"string"},"interactive":{"type":"boolean"}},"required":["command"]}`)
}

func (s *stubShellTool) Interactive(argsJSON string) bool {
	var args struct {
		Interactive bool `json:"interactive"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return false
	}
	return args.Interactive
}

func (s *stubShellTool) Invoke(ctx context.Context, argsJSON string) ToolResult {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ToolResult{Text: fmt.Sprintf(MsgParseArgs, err)}
	}
	if ctx.Err() != nil {
		return ToolResult{Text: stubInterruptNotStarted}
	}
	if strings.HasPrefix(args.Command, "sleep") {
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
			return ToolResult{Text: stubInterruptRunning}
		}
	}
	return ToolResult{Text: "stdout:\n" + args.Command + "\n", Meta: &stubShellMeta{Command: args.Command}}
}

type stubCalcTool struct{}

func (s *stubCalcTool) Name() string { return "calc" }

func (s *stubCalcTool) Definition() ToolDef {
	return NewToolDef(s.Name(), "测试桩：计算器", `{"type":"object","properties":{"expression":{"type":"string"}},"required":["expression"]}`)
}

func (s *stubCalcTool) Invoke(context.Context, string) ToolResult { return ToolResult{Text: "42"} }

func stubTools() []Tool { return []Tool{&stubShellTool{}, &stubCalcTool{}} }

func newAgent(t *testing.T, m *mockLLM, opts ...Option) *Agent {
	t.Helper()
	opts = append([]Option{WithTools(stubTools()...)}, opts...)
	a, err := New(m.config(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func newAgentWithCfg(t *testing.T, cfg *Config, opts ...Option) *Agent {
	t.Helper()
	opts = append([]Option{WithTools(stubTools()...)}, opts...)
	a, err := New(cfg, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
