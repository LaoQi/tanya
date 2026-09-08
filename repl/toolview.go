package repl

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
	"github.com/LaoQi/tanyan/style"
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
	budget := width - 5 - style.Width(name)
	return fmt.Sprintf("\n▸ %s %s ⋯\n", name, style.Truncate(toolArgsDisplay(name, args), budget))
}

func toolEndTitle(name, args string, res agent.ToolResult) string {
	title := name
	if disp := toolArgsDisplay(name, args); disp != "" {
		title += "  " + disp
	}
	return title
}

func toolEndBody(res agent.ToolResult, width, maxLines int) string {
	var b strings.Builder
	var lines []string
	status := ""
	if res.Shell != nil {
		var total int
		var trunc bool
		lines, status, total, trunc = shellView(res.Shell, width, maxLines)
		parts := []string{status, respDuration(res.Shell.Duration)}
		switch {
		case trunc:
			parts = append(parts, fmt.Sprintf(MsgLinesTotal, total))
		case total > 0:
			parts = append(parts, fmt.Sprintf(MsgLines, total))
		}
		status = strings.Join(parts, " · ")
	} else {
		lines, status = textView(res.Text, width, maxLines)
	}
	for _, l := range lines {
		b.WriteString("  " + l + "\n")
	}
	if status != "" {
		b.WriteString(style.Info.Sprint("  ↳ "+status) + "\n")
	}
	return b.String()
}

func RenderToolEnd(name, args string, res agent.ToolResult, width, maxLines int) string {
	return "\n▸ " + style.Truncate(toolEndTitle(name, args, res), width-2) + "\n" + toolEndBody(res, width, maxLines)
}

func RenderToolEndInline(name, args string, res agent.ToolResult, width, maxLines int) string {
	return style.CursorUp(1) + style.ClearLineHome() + "▸ " + style.Truncate(toolEndTitle(name, args, res), width-2) + "\n" + toolEndBody(res, width, maxLines)
}

func RenderResponseInfo(info agent.ResponseInfo, width int) string {
	var parts []string
	if info.TTFT > 0 {
		parts = append(parts, "TTFT "+respDuration(info.TTFT))
	}
	if info.Duration > 0 {
		parts = append(parts, respDuration(info.Duration))
	}
	if info.Usage != nil {
		u := info.Usage
		parts = append(parts, "prompt "+shortTokens(u.PromptTokens))
		if u.CompletionTokens > 0 {
			parts = append(parts, "completion "+shortTokens(u.CompletionTokens))
		}
		if hit := u.CacheHit(); hit > 0 && u.PromptTokens > 0 {
			parts = append(parts, fmt.Sprintf(MsgCachePct, float64(hit)/float64(u.PromptTokens)*100))
		}
	} else if info.ContextTokens > 0 {
		parts = append(parts, fmt.Sprintf(MsgCtxTokens, shortTokens(info.ContextTokens)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  ↳ " + style.Truncate(strings.Join(parts, " · "), width-4) + "\n"
}

func respDuration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

func shortTokens(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
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

func shellView(r *agent.ShellResult, width, maxLines int) ([]string, string, int, bool) {
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
	trunc := false
	if total > maxLines {
		if toolHeadLines+toolTailLines >= total {
			view = tagged
		} else {
			view = append(append([]tagLine{}, tagged[:toolHeadLines]...), tagged[total-toolTailLines:]...)
			trunc = true
		}
	}
	lines := make([]string, 0, len(view))
	for _, t := range view {
		s := t.text
		if t.stderr {
			s = "2| " + s
		}
		lines = append(lines, style.Truncate(s, width-2))
	}
	return lines, shellStatus(r), total, trunc
}

func chunkLines(chunks []agent.ShellChunk) []string {
	var out []string
	for _, c := range chunks {
		if c.Truncated > 0 {
			out = append(out, fmt.Sprintf(MsgTruncNote, c.Truncated))
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
		return MsgInterrupt
	case r.TimedOut:
		return MsgTimeout
	case r.Err != "":
		return fmt.Sprintf(MsgToolErr, r.Err)
	}
	return fmt.Sprintf("exit %d", r.ExitCode)
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
		out[i] = style.Truncate(l, width-2)
	}
	if trunc {
		return out, fmt.Sprintf(MsgLinesTotal, len(lines))
	}
	return out, ""
}

func WireToolView(a *agent.Agent, width func() int, maxLines int) func(string) {
	var mu sync.Mutex
	sp := newSpinner(&mu, style.GetProfile().TTY)
	toolJustEnded := false
	lineDirty := false
	a.OnRequestStart = func() {
		sp.start(func(elapsed time.Duration, frame string) string {
			return style.Warn.Sprint(fmt.Sprintf(SpinWaiting, frame, spinElapsed(elapsed)))
		})
	}
	a.OnResponse = func(info agent.ResponseInfo) {
		sp.stop()
		mu.Lock()
		if lineDirty {
			fmt.Println()
			lineDirty = false
		}
		fmt.Print(style.Info.Sprint(RenderResponseInfo(info, width())))
		mu.Unlock()
	}
	a.OnToolStart = func(name, args string) {
		sp.stop()
		mu.Lock()
		fmt.Print(style.Dim.Sprint(RenderToolStart(name, args, width())))
		mu.Unlock()
		lineDirty = false
		sp.start(func(elapsed time.Duration, frame string) string {
			return style.Warn.Sprint(fmt.Sprintf(SpinRunning, frame, spinElapsed(elapsed)))
		})
	}
	a.OnToolEnd = func(name, args string, res agent.ToolResult) {
		sp.stop()
		mu.Lock()
		if style.GetProfile().TTY {
			fmt.Print(style.Dim.Sprint(RenderToolEndInline(name, args, res, width(), maxLines)))
		} else {
			fmt.Print(RenderToolEnd(name, args, res, width(), maxLines))
		}
		mu.Unlock()
		toolJustEnded = true
		lineDirty = false
	}
	return func(s string) {
		if s == "" {
			return
		}
		sp.stop()
		mu.Lock()
		if toolJustEnded {
			fmt.Println()
			toolJustEnded = false
		}
		fmt.Print(s)
		lineDirty = !strings.HasSuffix(s, "\n")
		mu.Unlock()
	}
}

var toolTerm readline.Terminal
var toolTTY bool

func ToolWidth() int {
	ensureToolTerm()
	return toolWidth(toolTerm)
}

func ToolTTY() bool {
	ensureToolTerm()
	return toolTTY
}

func ensureToolTerm() {
	if toolTerm == nil {
		toolTerm, toolTTY = readline.NewTerminal()
	}
}
