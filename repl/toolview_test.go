package repl

import (
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanyan/agent"
)

func TestRenderToolStart(t *testing.T) {
	got := RenderToolStart("run_shell", `{"command":"ls -la","timeout":60}`, 80)
	if !strings.Contains(got, "● run_shell") || !strings.Contains(got, "ls -la") || !strings.Contains(got, "⋯") {
		t.Errorf("got %q", got)
	}
	if !strings.HasPrefix(got, "\n") || !strings.HasSuffix(got, "\n") {
		t.Errorf("应有前后空行包裹: %q", got)
	}
}

func TestRenderToolStartBadJSON(t *testing.T) {
	got := RenderToolStart("run_shell", `{bad`, 80)
	if !strings.Contains(got, `{bad`) {
		t.Errorf("解析失败应原样显示: %q", got)
	}
}

func TestRenderToolEndShortOutput(t *testing.T) {
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout:   []agent.ShellChunk{{Data: "line1\nline2\nline3\n"}},
		ExitCode: 0,
	}}
	got := RenderToolEnd("run_shell", `{"command":"ls"}`, res, 80, 20)
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line3") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "省略") || strings.Contains(got, "exit") {
		t.Errorf("短输出不应有截断提示或状态行: %q", got)
	}
}

func TestRenderToolEndLongOutput(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 30; i++ {
		sb.WriteString("L" + strings.Repeat("x", i) + "\n")
	}
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout: []agent.ShellChunk{{Data: sb.String()}},
	}}
	got := RenderToolEnd("run_shell", `{"command":"seq"}`, res, 80, 20)
	if strings.Contains(got, "L4x\n") || strings.Contains(got, "L28") {
		t.Errorf("中段行应被省略: %q", got)
	}
	if !strings.Contains(got, "Lx\n") || !strings.Contains(got, strings.Repeat("x", 29)) {
		t.Errorf("应保留首行 Lx 与末行 L+29x: %q", got)
	}
	if !strings.Contains(got, "共 30 行") {
		t.Errorf("应含总数提示: %q", got)
	}
}

func TestRenderToolEndFailStatus(t *testing.T) {
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stderr:   []agent.ShellChunk{{Data: "oops"}},
		ExitCode: 2,
	}}
	got := RenderToolEnd("run_shell", `{"command":"false"}`, res, 80, 20)
	if !strings.Contains(got, "2| oops") {
		t.Errorf("stderr 应带 2| 标记: %q", got)
	}
	if !strings.Contains(got, "exit 2") {
		t.Errorf("应有退出码状态: %q", got)
	}
}

func TestRenderToolEndTimeoutInterrupt(t *testing.T) {
	res := agent.ToolResult{Shell: &agent.ShellResult{TimedOut: true, Duration: 3 * time.Second}}
	got := RenderToolEnd("run_shell", `{"command":"sleep"}`, res, 80, 20)
	if !strings.Contains(got, "执行超时") || !strings.Contains(got, "3.0s") {
		t.Errorf("got %q", got)
	}
	res2 := agent.ToolResult{Shell: &agent.ShellResult{Interrupted: true}}
	if got := RenderToolEnd("run_shell", `{}`, res2, 80, 20); !strings.Contains(got, "已中断") {
		t.Errorf("got %q", got)
	}
}

func TestRenderToolEndTruncateLongLine(t *testing.T) {
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout: []agent.ShellChunk{{Data: strings.Repeat("a", 200) + "\n"}},
	}}
	got := RenderToolEnd("run_shell", `{"command":"cat"}`, res, 80, 20)
	if n := strings.Count(got, "\n"); n != 3 {
		t.Errorf("应为前导空行+标题+正文 3 行，实际 %d: %q", n, got)
	}
	if !strings.Contains(got, "~") {
		t.Errorf("超长行应以 ~ 结尾截断: %q", got)
	}
}

func TestRenderToolEndBuiltin(t *testing.T) {
	res := agent.ToolResult{Text: "1700000000 +0800 CST"}
	got := RenderToolEnd("get_time", `{}`, res, 80, 20)
	if !strings.Contains(got, "● get_time") || !strings.Contains(got, "1700000000") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "{}") {
		t.Errorf("非 shell 工具不应显示空参数: %q", got)
	}
}

func TestRenderToolEndBuiltinError(t *testing.T) {
	res := agent.ToolResult{Text: "error: 除数为零"}
	got := RenderToolEnd("calc", `{"expression":"1/0"}`, res, 80, 20)
	if !strings.Contains(got, "error: 除数为零") {
		t.Errorf("got %q", got)
	}
}

func TestRenderToolEndTruncationMarker(t *testing.T) {
	res := agent.ToolResult{Shell: &agent.ShellResult{
		Stdout: []agent.ShellChunk{
			{Data: "head\n"},
			{Data: "tail\n", Truncated: 9999},
		},
	}}
	got := RenderToolEnd("run_shell", `{}`, res, 80, 20)
	if !strings.Contains(got, "中间省略 9999 字节") {
		t.Errorf("应显示中间截断标记: %q", got)
	}
}

func TestToolWidthFallback(t *testing.T) {
	if w := ToolWidth(); w <= 0 {
		t.Errorf("宽度应回退 80: %d", w)
	}
}
