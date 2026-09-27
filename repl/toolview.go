package repl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"
	"io"
	"strings"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/present"
)

const (
	toolArgsPrefix = "  "
	toolArgsSep    = " · "
)

func RenderToolStart(name, args string, views *present.Registry, width int) string {
	lines := toolTitleLines(name, args, views, width)
	var b strings.Builder
	b.WriteString("\n▸ " + lines[0] + "\n")
	for _, l := range lines[1:] {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// toolTitleLines 组装标题区：参数短到能与工具名同行时内联单行（`▸ run_shell ls -la`、`▸ calc expression: 6*7`），
// 放不下或参数多行/多值时转块形态——首行工具名，其后是参数区（run_shell 为 cwd/timeout 行加 `  $ ` 命令行，
// 其余工具为 `  key: value` 行）。width 是终端总列数，各前缀宽度在此扣除。
func toolTitleLines(name, args string, views *present.Registry, width int) []string {
	v := toolArgsView(name, args, views, width)
	if v.Inline != "" {
		if inner := width - 3 - term.Width(name) - 1; inner > 0 && term.Width(v.Inline) <= inner {
			return []string{term.Truncate(name+" "+v.Inline, width-3)}
		}
	}
	return append([]string{term.Truncate(name, width-3)}, v.Body...)
}

// toolArgsView 按工具名分派参数视图：注册了自带视图的工具（如 run_shell）走它的实现，
// 其余（含未来的新工具）走通用键值渲染。JSON 解析失败一律退回原样展示，参数为空则只显示工具名。
func toolArgsView(name, args string, views *present.Registry, width int) present.ArgsView {
	if v, ok := views.Args(name, args, width); ok {
		return v
	}
	return genericArgsView(args, width)
}

// genericArgsView 渲染非 shell 工具的参数：按模型给出的键序逐项 `key: value`，内联用 ` · ` 连接，
// 放不下或多行时转块形态（每项一行、按宽度折行）。
func genericArgsView(args string, width int) present.ArgsView {
	pairs, ok := parseArgPairs(args)
	if !ok {
		return present.PlainArgsView(present.TrimBlankEdges(args), toolArgsPrefix, MsgArgsOmittedFmt, width)
	}
	if len(pairs) == 0 {
		return present.ArgsView{}
	}
	parts := make([]string, len(pairs))
	var wrapped []string
	for i, p := range pairs {
		parts[i] = fmt.Sprintf(MsgArgPairFmt, p.key, p.value)
		wrapped = append(wrapped, term.Wrap(parts[i], width-len(toolArgsPrefix))...)
	}
	v := present.ArgsView{Body: present.Prefixed(present.CapLines(wrapped, width-len(toolArgsPrefix), MsgArgsOmittedFmt), toolArgsPrefix)}
	if inline := strings.Join(parts, toolArgsSep); !strings.Contains(inline, "\n") {
		v.Inline = inline
	}
	return v
}

func toolEndBody(name string, res agent.ToolResult, views *present.Registry, width, maxLines int) (string, string) {
	if v, ok := views.Result(name, res.Text, res.Meta, width, maxLines); ok {
		return v.Body, v.Status
	}
	lines, status := textView(res.Text, width, maxLines)
	return present.Indent(lines), status
}

// RenderToolEndAppend 追加工具正文块与状态行：标题已由 RenderToolStart 打出一次，此处不重复。
func RenderToolEndAppend(sem theme.Semantics, prof term.Profile, name string, res agent.ToolResult, views *present.Registry, width, maxLines int) string {
	out, status := toolEndBody(name, res, views, width, maxLines)
	var b strings.Builder
	if term.HasSGR(out) {
		b.WriteString(term.Passthrough(prof, out))
	} else {
		b.WriteString(sem.Dim.With(prof).Frame(out))
	}
	if status != "" {
		b.WriteString(sem.Info.With(prof).Sprint("  ↳ "+term.Strip(status)) + "\n")
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

func respDuration(d time.Duration) string { return present.Duration(d) }

type argPair struct {
	key   string
	value string
}

// parseArgPairs 按 JSON 原文顺序取出顶层键值（Unmarshal 到 map 会按字母序重排，模型给的 schema 顺序更可读）。
// 非对象或解析失败时返回 ok=false。
func parseArgPairs(args string) ([]argPair, bool) {
	dec := json.NewDecoder(strings.NewReader(args))
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, false
	}
	var out []argPair
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := kt.(string)
		if !ok {
			return nil, false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false
		}
		out = append(out, argPair{key: key, value: argDisplayValue(raw)})
	}
	if _, err := dec.Token(); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return out, true
}

// argDisplayValue 把参数值转成展示文本：字符串原样（含多行、制表符摊平）、标量数组顿号连接、
// 对象与嵌套结构压成紧凑 JSON、null 显式写出。
func argDisplayValue(raw json.RawMessage) string {
	t := strings.TrimSpace(string(raw))
	switch {
	case t == "":
		return ""
	case t == "null":
		return "null"
	case t[0] == '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return compactJSON(raw)
		}
		if s == "" {
			return `""`
		}
		return present.ExpandTabs(present.TrimBlankEdges(s))
	case t[0] == '[':
		var list []json.RawMessage
		if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
			return compactJSON(raw)
		}
		parts := make([]string, 0, len(list))
		for _, e := range list {
			et := strings.TrimSpace(string(e))
			if et == "" || et[0] == '{' || et[0] == '[' {
				return compactJSON(raw)
			}
			parts = append(parts, argDisplayValue(e))
		}
		return strings.Join(parts, ", ")
	}
	return compactJSON(raw)
}

func compactJSON(raw json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return b.String()
}

// trimBlankEdges 去掉首尾空行但保留行首缩进——heredoc/多行脚本的缩进是命令结构的一部分。
func trimBlankEdges(s string) string { return strings.Trim(s, "\n\r") }

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
	views     *present.Registry
	st        *streams
	heart     *heartbeat
	prof      term.Profile
	sem       theme.Semantics
	width     func() int
	maxLines  int
	justEnded bool
	dirty     bool
}

func NewToolView(st *streams, prof term.Profile, sem theme.Semantics, width func() int, maxLines int, views *present.Registry) *toolView {
	return &toolView{
		views:    views,
		st:       st,
		heart:    newHeartbeat(st.out, sem, prof),
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
		v.Content(KindContent, term.Sanitize(e.Text, false))
	case agent.EventResponse:
		v.heart.stop()
		dirty := v.dirty
		v.st.out.atomic(KindToolStatus, func(w io.Writer) {
			if dirty {
				io.WriteString(w, "\n")
			}
			io.WriteString(w, v.sem.Info.With(v.prof).Sprint(RenderResponseInfo(e.Response, v.width())))
		})
		v.dirty = false
	case agent.EventToolStart:
		v.heart.stop()
		v.st.out.atomic(KindToolBlock, func(w io.Writer) {
			io.WriteString(w, v.sem.Dim.With(v.prof).Frame(RenderToolStart(e.ToolName, e.ToolArgs, v.views, v.width())))
			if e.Interactive {
				io.WriteString(w, v.sem.Info.With(v.prof).Sprint(MsgInteractiveHint))
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
			block := RenderToolEndAppend(v.sem, v.prof, e.ToolName, e.Result, v.views, v.width(), v.maxLines)
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
