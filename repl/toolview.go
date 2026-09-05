package repl

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
)

const (
	toolHeadLines = 3
	toolTailLines = 2
)

type tagLine struct {
	text   string
	stderr bool
}

func toolWidth(term readline.Terminal) int {
	if term != nil {
		if s, ok := term.Size(); ok && s.Cols > 0 {
			return s.Cols
		}
	}
	return 80
}

func RenderToolStart(name, args string, width int) string {
	return fmt.Sprintf("\n● %s %s ⋯\n", name, readline.Truncate(toolArgsDisplay(name, args), width))
}

func RenderToolEnd(name, args string, res agent.ToolResult, width, maxLines int) string {
	var b strings.Builder
	title := name
	if disp := toolArgsDisplay(name, args); disp != "" {
		title += "  " + disp
	}
	if res.Shell != nil {
		if d := toolDuration(res.Shell.Duration); d != "" {
			title += "  " + d
		}
	}
	b.WriteString("\n● " + readline.Truncate(title, width) + "\n")
	var lines []string
	status := ""
	if res.Shell != nil {
		lines, status = shellView(res.Shell, width, maxLines)
	} else {
		lines, status = textView(res.Text, width, maxLines)
	}
	for _, l := range lines {
		b.WriteString("  " + l + "\n")
	}
	if status != "" {
		b.WriteString("  ↳ " + status + "\n")
	}
	return b.String()
}

func toolArgsDisplay(name, args string) string {
	if name != "run_shell" {
		return ""
	}
	var a struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(args), &a); err == nil && strings.TrimSpace(a.Command) != "" {
		return a.Command
	}
	return args
}

func toolDuration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("(%.1fs)", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("(%dms)", d.Milliseconds())
	}
	return ""
}

func shellView(r *agent.ShellResult, width, maxLines int) ([]string, string) {
	stdoutLines := chunkLines(r.Stdout)
	stderrLines := chunkLines(r.Stderr)
	total := len(stdoutLines) + len(stderrLines)
	tagged := make([]tagLine, 0, total)
	for _, l := range stdoutLines {
		tagged = append(tagged, tagLine{l, false})
	}
	for _, l := range stderrLines {
		tagged = append(tagged, tagLine{l, true})
	}
	view := tagged
	trunc := total > maxLines
	if trunc {
		if toolHeadLines+toolTailLines >= total {
			view = tagged
			trunc = false
		} else {
			view = append(append([]tagLine{}, tagged[:toolHeadLines]...), tagged[total-toolTailLines:]...)
		}
	}
	lines := make([]string, 0, len(view))
	for _, t := range view {
		s := t.text
		if t.stderr {
			s = "2| " + s
		}
		lines = append(lines, readline.Truncate(s, width))
	}
	status := shellStatus(r)
	if trunc {
		status += fmt.Sprintf("（共 %d 行，已省略部分，完整输出 /history n）", total)
	}
	return lines, status
}

func chunkLines(chunks []agent.ShellChunk) []string {
	var out []string
	for _, c := range chunks {
		if c.Truncated > 0 {
			out = append(out, fmt.Sprintf("…中间省略 %d 字节…", c.Truncated))
		}
		if c.Data == "" {
			continue
		}
		out = append(out, strings.Split(strings.TrimRight(c.Data, "\n"), "\n")...)
	}
	return out
}

func shellStatus(r *agent.ShellResult) string {
	switch {
	case r.Interrupted:
		return "已中断"
	case r.TimedOut:
		return "执行超时"
	case r.Err != "":
		return "错误: " + r.Err
	case r.ExitCode != 0:
		return fmt.Sprintf("exit %d", r.ExitCode)
	}
	return ""
}

func textView(text string, width, maxLines int) ([]string, string) {
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil, ""
	}
	lines := strings.Split(text, "\n")
	trunc := len(lines) > maxLines
	view := lines
	if trunc {
		view = lines[:maxLines]
	}
	out := make([]string, len(view))
	for i, l := range view {
		out[i] = readline.Truncate(l, width)
	}
	if trunc {
		return out, fmt.Sprintf("已省略 %d 行，完整内容 /history n", len(lines)-maxLines)
	}
	return out, ""
}

func WireToolView(a *agent.Agent, width func() int, maxLines int) func(string) {
	toolJustEnded := false
	a.OnToolStart = func(name, args string) {
		fmt.Print(RenderToolStart(name, args, width()))
	}
	a.OnToolEnd = func(name, args string, res agent.ToolResult) {
		fmt.Print(RenderToolEnd(name, args, res, width(), maxLines))
		toolJustEnded = true
	}
	return func(s string) {
		if toolJustEnded && s != "" {
			fmt.Println()
			toolJustEnded = false
		}
		fmt.Print(s)
	}
}

var toolTerm readline.Terminal

func ToolWidth() int {
	if toolTerm == nil {
		toolTerm, _ = readline.NewTerminal()
	}
	return toolWidth(toolTerm)
}
