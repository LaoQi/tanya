package repl

import (
	"encoding/json"
	"fmt"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"
	"io"
	"strings"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
)

const (
	toolHeadLines = 3
	toolTailLines = 2

	toolCommandMaxLines  = 8
	toolCommandHeadLines = 6
	toolCommandTailLines = 1

	toolCommandPrefix = "  $ "
	toolCwdPrefix     = "  cwd: "
	toolTabWidth      = 4
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
	lines := toolTitleLines(name, args, width)
	var b strings.Builder
	b.WriteString("\n▸ " + lines[0] + "\n")
	for _, l := range lines[1:] {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// toolTitleLines 组装标题区：命令短到能与工具名同行时内联单行（`▸ run_shell ls -la`，与旧版逐字节一致），
// 否则转块形态——首行工具名，其后是 cwd 行（显式指定时）与折行的命令区（`  $ ` 前缀，与输出区区分）。
// 命令行数超上限时省略中段并提示 /history，见 commandLines。width 是终端总列数，各前缀宽度在此扣除。
func toolTitleLines(name, args string, width int) []string {
	cwd, cmd := toolArgsDisplay(name, args)
	if cwd == "" && cmd != "" {
		if inner := width - 3 - term.Width(name) - 1; inner > 0 && !strings.Contains(cmd, "\n") && term.Width(cmd) <= inner {
			return []string{term.Truncate(name+" "+cmd, width-3)}
		}
	}
	lines := []string{term.Truncate(name, width-3)}
	if cwd != "" {
		lines = append(lines, term.Truncate(toolCwdPrefix+cwd, width-2))
	}
	for _, l := range commandLines(cmd, width-len(toolCommandPrefix)) {
		lines = append(lines, toolCommandPrefix+l)
	}
	return lines
}

// commandLines 把命令折成显示行（制表符已摊平、保留原换行结构）；超过上限时保留头尾，
// 中段换成省略提示——命令是有序脚本，省略中段比省略尾部更不易误读收尾的 done/EOF。
func commandLines(cmd string, width int) []string {
	if cmd == "" {
		return nil
	}
	lines := term.Wrap(cmd, width)
	if len(lines) <= toolCommandMaxLines {
		return lines
	}
	omitted := len(lines) - toolCommandHeadLines - toolCommandTailLines
	out := make([]string, 0, toolCommandMaxLines)
	out = append(out, lines[:toolCommandHeadLines]...)
	out = append(out, term.Truncate(fmt.Sprintf(MsgCmdOmittedFmt, omitted), width))
	return append(out, lines[len(lines)-toolCommandTailLines:]...)
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

// RenderToolEndAppend 追加工具正文块与状态行：标题已由 RenderToolStart 打出一次，此处不重复。
func RenderToolEndAppend(sem theme.Semantics, res agent.ToolResult, width, maxLines int) string {
	out, status := toolEndBody(res, width, maxLines)
	var b strings.Builder
	if term.HasSGR(out) {
		b.WriteString(term.Passthrough(out))
	} else {
		b.WriteString(sem.Dim.Frame(out))
	}
	if status != "" {
		b.WriteString(sem.Info.Sprint("  ↳ "+term.Strip(status)) + "\n")
	}
	return b.String()
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
		parts = append(parts, "prompt "+formatTokens(u.PromptTokens))
		if u.CompletionTokens > 0 {
			parts = append(parts, "completion "+formatTokens(u.CompletionTokens))
		}
		if rate := formatRate(u.CacheHit(), u.PromptTokens); rate != "" {
			parts = append(parts, fmt.Sprintf(MsgCachePct, rate))
		}
	} else if info.ContextTokens > 0 {
		parts = append(parts, fmt.Sprintf(MsgCtxTokens, formatTokens(info.ContextTokens)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  ↳ " + term.Truncate(strings.Join(parts, " · "), width-4) + "\n"
}

func respDuration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

func toolArgsDisplay(name, args string) (string, string) {
	if name != "run_shell" {
		return "", ""
	}
	var a struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil || strings.TrimSpace(a.Command) == "" {
		return "", trimBlankEdges(args)
	}
	return strings.TrimSpace(a.Cwd), expandTabs(trimBlankEdges(a.Command))
}

// trimBlankEdges 去掉首尾空行但保留行首缩进——heredoc/多行脚本的缩进是命令结构的一部分。
func trimBlankEdges(s string) string { return strings.Trim(s, "\n\r") }

// expandTabs 展开制表符：宽度表把 \t 当单列，与终端制表位不符，折行前必须先摊平，否则折行位置与显示不符。
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	return strings.ReplaceAll(s, "\t", strings.Repeat(" ", toolTabWidth))
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
		lines = append(lines, term.Truncate(s, width-2))
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
		out[i] = term.Truncate(l, width-2)
	}
	if trunc {
		return out, fmt.Sprintf(MsgLinesTotal, len(lines))
	}
	return out, ""
}

// toolView 是工具区渲染器：闭包状态提为字段，仍是 agent.EventSink（Handle 即签名匹配）。
type toolView struct {
	st        *streams
	heart     *heartbeat
	prof      term.Profile
	sem       theme.Semantics
	width     func() int
	maxLines  int
	justEnded bool
	dirty     bool
}

func NewToolView(st *streams, prof term.Profile, sem theme.Semantics, width func() int, maxLines int) *toolView {
	return &toolView{
		st:       st,
		heart:    newHeartbeat(st.out, sem),
		prof:     prof,
		sem:      sem,
		width:    width,
		maxLines: maxLines,
	}
}

func (v *toolView) setSemantics(sem theme.Semantics) {
	v.sem = sem
	v.heart.setSemantics(sem)
}

// statusOn 报告是否展示过程状态行：TTY 且当前模式放行 KindStatus（仅 rich 档）。
func (v *toolView) statusOn() bool { return v.prof.TTY && v.st.out.allows(KindStatus) }

// Stop 收尾进行中的心跳（幂等）：无进行中的心跳时不写任何字节。
func (v *toolView) Stop() { v.heart.stop() }

// Content 输出正文（原 REPL.print 的语义）：停心跳、若上一块是工具块先补空行、跟踪行尾状态。
func (v *toolView) Content(kind Kind, text string) {
	if text == "" {
		return
	}
	v.heart.stop()
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
		v.heart.start(statusWaiting, v.statusOn())
	case agent.EventReasoning:
		v.heart.setPhase(statusThinking)
	case agent.EventContent:
		v.Content(KindContent, e.Text)
	case agent.EventResponse:
		v.heart.stop()
		dirty := v.dirty
		v.st.out.atomic(KindToolStatus, func(w io.Writer) {
			if dirty {
				io.WriteString(w, "\n")
			}
			io.WriteString(w, v.sem.Info.Sprint(RenderResponseInfo(e.Response, v.width())))
		})
		v.dirty = false
	case agent.EventToolStart:
		v.heart.stop()
		v.st.out.atomic(KindToolBlock, func(w io.Writer) {
			io.WriteString(w, v.sem.Dim.Frame(RenderToolStart(e.ToolName, e.ToolArgs, v.width())))
			if e.Interactive {
				io.WriteString(w, v.sem.Info.Sprint(MsgInteractiveHint))
			}
		})
		v.dirty = false
		if !e.Interactive {
			v.heart.start(statusToolRunning, v.statusOn())
		}
	case agent.EventToolEnd:
		v.heart.stop()
		// 工具块被屏蔽时不置 justEnded：否则下一条正文前会留下孤立空行。
		if v.st.out.allows(KindToolBlock) {
			block := RenderToolEndAppend(v.sem, e.Result, v.width(), v.maxLines)
			if e.Interactive {
				block = "\n" + block
			}
			v.st.out.atomic(KindToolBlock, func(w io.Writer) {
				io.WriteString(w, block)
			})
			v.justEnded = true
		}
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
