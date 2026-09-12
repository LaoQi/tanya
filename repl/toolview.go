package repl

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
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
	budget := width - 6 - style.Width(name)
	return fmt.Sprintf("\n▸ %s %s ⋯\n", name, style.Truncate(toolArgsDisplay(name, args), budget))
}

func toolEndTitle(name, args string, res agent.ToolResult) string {
	title := name
	if disp := toolArgsDisplay(name, args); disp != "" {
		title += " " + disp
	}
	return title
}

func toolEndBody(res agent.ToolResult, width, maxLines int) (string, string) {
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
	return b.String(), status
}

func renderToolBlock(lead, name, args string, res agent.ToolResult, width, maxLines int) string {
	title := style.Truncate(toolEndTitle(name, args, res), width-3)
	out, status := toolEndBody(res, width, maxLines)
	var b strings.Builder
	b.WriteString(lead)
	if style.HasSGR(out) {
		b.WriteString(style.Dim.Frame("▸ " + title + "\n"))
		b.WriteString(style.Passthrough(out))
	} else {
		b.WriteString(style.Dim.Frame("▸ " + title + "\n" + out))
	}
	if status != "" {
		b.WriteString(style.Info.Sprint("  ↳ "+status) + "\n")
	}
	return b.String()
}

func RenderToolEnd(name, args string, res agent.ToolResult, width, maxLines int) string {
	return renderToolBlock("\n", name, args, res, width, maxLines)
}

func RenderToolEndInline(name, args string, res agent.ToolResult, width, maxLines int) string {
	return renderToolBlock(style.CursorUp(1)+style.ClearLineHome(), name, args, res, width, maxLines)
}

func RenderResponseInfo(info agent.ResponseInfo, width int) string {
	var parts []string
	if info.FirstEvent > 0 {
		parts = append(parts, "TTFT "+respDuration(info.FirstEvent))
	}
	if info.FirstContent > info.FirstEvent {
		parts = append(parts, "TTFC "+respDuration(info.FirstContent))
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
		return collapseCommand(a.Command)
	}
	return collapseCommand(args)
}

func collapseCommand(s string) string {
	var parts []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, "; ")
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
	case r.Interrupted && r.NotStarted:
		return MsgNotStarted
	case r.Interrupted:
		return MsgInterrupt
	case r.TimedOut:
		return MsgTimeout
	case r.Stopped:
		return MsgSuspended
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

// toolView 是工具区渲染器：闭包状态提为字段，仍是 agent.EventSink（Handle 即签名匹配）。
type toolView struct {
	st        *streams
	sp        *spinner
	prof      style.Profile
	width     func() int
	maxLines  int
	justEnded bool
	dirty     bool
}

func NewToolView(st *streams, prof style.Profile, width func() int, maxLines int) *toolView {
	return &toolView{
		st:       st,
		sp:       newSpinner(st.out, prof.TTY && st.out.allows(KindSpinner)),
		prof:     prof,
		width:    width,
		maxLines: maxLines,
	}
}

// Content 输出正文（原 REPL.print 的语义）：停动画、若上一块是工具块先补空行、跟踪行尾状态。
func (v *toolView) Content(kind Kind, text string) {
	if text == "" {
		return
	}
	v.sp.stop()
	justEnded := v.justEnded
	v.st.out.atomic(kind, func(w io.Writer) {
		if justEnded {
			io.WriteString(w, "\n")
		}
		io.WriteString(w, text)
	})
	v.justEnded = false
	v.dirty = !strings.HasSuffix(text, "\n")
}

func (v *toolView) Handle(e agent.Event) {
	switch e.Kind {
	case agent.EventRequestStart:
		v.sp.start(spinWaiting)
	case agent.EventReasoning:
		v.sp.setKind(spinThinking)
	case agent.EventContent:
		v.Content(KindContent, e.Text)
	case agent.EventResponse:
		v.sp.stop()
		dirty := v.dirty
		v.st.out.atomic(KindToolStatus, func(w io.Writer) {
			if dirty {
				io.WriteString(w, "\n")
			}
			io.WriteString(w, style.Info.Sprint(RenderResponseInfo(e.Response, v.width())))
		})
		v.dirty = false
	case agent.EventToolStart:
		v.sp.stop()
		v.st.out.atomic(KindToolBlock, func(w io.Writer) {
			io.WriteString(w, style.Dim.Frame(RenderToolStart(e.ToolName, e.ToolArgs, v.width())))
			if e.Interactive {
				io.WriteString(w, style.Info.Sprint(MsgInteractiveHint))
			}
		})
		v.dirty = false
		if !e.Interactive {
			v.sp.start(spinRunning)
		}
	case agent.EventToolEnd:
		v.sp.stop()
		block := RenderToolEnd(e.ToolName, e.ToolArgs, e.Result, v.width(), v.maxLines)
		if v.prof.TTY && !e.Interactive {
			block = RenderToolEndInline(e.ToolName, e.ToolArgs, e.Result, v.width(), v.maxLines)
		}
		v.st.out.atomic(KindToolBlock, func(w io.Writer) {
			io.WriteString(w, block)
		})
		v.justEnded = true
		v.dirty = false
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
