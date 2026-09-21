package repl

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanya/render/term"
)

func TestUsageModeLinesAligned(t *testing.T) {
	lines := []struct {
		label string
		desc  string
	}{
		{"  tanya", "交互 REPL（默认）"},
		{`  tanya ask "问题"`, "单发提问"},
		{"  tanya init", "新工作区脚手架"},
		{"  tanya config", "把内置默认配置示例"},
	}
	col := -1
	for _, c := range lines {
		line := usageLine(t, c.label, c.desc)
		got := term.Width(line[:strings.Index(line, c.desc)])
		if col < 0 {
			col = got
			continue
		}
		if got != col {
			t.Errorf("%s 的描述起始列 = %d，应为 %d（按显示宽度对齐，CJK 记 2 列）:\n%s", c.label, got, col, UsageHead)
		}
	}
}

func usageLine(t *testing.T, label, desc string) string {
	t.Helper()
	for _, line := range strings.Split(UsageHead, "\n") {
		if strings.HasPrefix(line, label) && strings.Contains(line, desc) {
			return line
		}
	}
	t.Fatalf("用法缺 %q 模式行:\n%s", label, UsageHead)
	return ""
}

func TestUsageMentionsEverySubcommand(t *testing.T) {
	for _, name := range []string{"ask", "init", "config"} {
		if cmd, _ := ParseCommand([]string{name}); cmd == CmdREPL {
			t.Fatalf("%s 是子命令，用例表已过期", name)
		}
		if !strings.Contains(UsageHead, "tanya "+name) {
			t.Errorf("用法应说明 tanya %s 模式:\n%s", name, UsageHead)
		}
	}
}
