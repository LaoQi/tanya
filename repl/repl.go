package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
	"github.com/LaoQi/tanyan/style"
)

type REPL struct {
	agent     *agent.Agent
	ed        *readline.Editor
	term      readline.Terminal
	raw       bool
	st        *streams
	prof      style.Profile
	promptTpl string
	prompt    style.Template
	view      agent.EventSink
	mdLive    bool
	rend      style.Renderer
}

type options struct {
	st   *streams
	term readline.Terminal
	raw  bool
}

type Option func(*options)

func WithStreams(st *streams) Option {
	return func(o *options) { o.st = st }
}

func WithTerminal(term readline.Terminal, raw bool) Option {
	return func(o *options) { o.term, o.raw = term, raw }
}

func NewREPL(a *agent.Agent, promptTpl string, opts ...Option) (*REPL, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.st == nil {
		o.st = NewStreams(os.Stdout, os.Stderr)
	}
	term, raw := o.term, o.raw
	if term == nil {
		term, raw = readline.NewTerminal()
	}
	ed := readline.NewEditor(term, raw)
	ed.SetOutput(o.st.out)
	c := &completer{listSessions: a.ListSessions, listModels: a.ListModels}
	ed.SetComplete(c.complete)
	ed.SetGhost(c.suggest)
	sch := style.CurrentScheme()
	if promptTpl == "" {
		promptTpl = sch.Prompt
	}
	tpl, err := style.ParseTemplate(promptTpl)
	if err != nil {
		return nil, err
	}
	r := &REPL{agent: a, ed: ed, term: term, raw: raw, st: o.st, promptTpl: promptTpl, prompt: tpl}
	r.mdLive = true
	r.prof = style.GetProfile()
	r.rend = style.NewThemedRenderer(r.prof, sch.MD)
	maxLines := 20
	if a != nil {
		maxLines = a.ToolOutputLines()
	}
	r.view = WireToolView(o.st, r.prof, func() int { return toolWidth(term) }, maxLines)
	return r, nil
}

func (r *REPL) print(text string) {
	r.view(agent.Event{Kind: agent.EventContent, Text: text})
}

func (r *REPL) mdEnabled() bool {
	return r.mdLive && r.prof.TTY
}

func turnSep(prof style.Profile, d time.Duration) string {
	if !prof.TTY {
		return ""
	}
	text := fmt.Sprintf(TurnSepTimeFmt, time.Now().Format("15:04:05"))
	if d > 0 {
		text += fmt.Sprintf(TurnSepDurFmt, turnDuration(d))
	}
	return "\n" + style.Ok.Sprint(text) + "\n"
}

func turnDuration(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d/time.Minute)%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int(d/time.Second)%60)
	case d >= time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

func (r *REPL) resolveVars() func(string) (string, bool) {
	return func(name string) (string, bool) {
		switch name {
		case "cwd":
			return r.cwdLabel(), true
		case "model":
			return r.agent.Model(), true
		case "effort":
			return r.agent.ReasoningEffort(), true
		case "usage":
			return r.agent.PromptUsage(), true
		case "cache":
			return r.agent.PromptCache(), true
		case "cache_rate":
			return r.agent.PromptCacheRate(), true
		case "stat":
			return r.agent.PromptSummary(), true
		}
		return "", false
	}
}

func (r *REPL) Close() {}

func (r *REPL) noSaveWarn() string {
	if r.agent == nil || !r.agent.NoSave() {
		return ""
	}
	return style.Warn.Sprint(MsgNoSaveWarn) + "\n"
}

func (r *REPL) Run() error {
	r.st.out.emit(KindDecor, welcomeText()+r.noSaveWarn())
	for {
		prompt := r.prompt.Render(r.resolveVars())
		line, err := r.ed.Readline(prompt)
		if err == readline.ErrInterrupt {
			continue
		}
		if err == io.EOF {
			r.st.out.emit(KindNotice, MsgBye+"\n")
			return nil
		}
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isExitLine(line) {
			r.st.out.emit(KindNotice, MsgBye+"\n")
			return nil
		}
		if isSlashCommand(line) {
			if r.handleCommand(line) {
				return nil
			}
			r.st.out.emit(KindDecor, turnSep(r.prof, 0))
			continue
		}
		if text, ok := dialogueText(line); ok {
			if text == "" {
				r.st.out.emit(KindNotice, MsgDialogueEmpty)
				continue
			}
			r.ask(text)
			continue
		}
		r.ask(line)
	}
}

func (r *REPL) ask(q string) {
	readline.SecureTerminal()
	ctx, done := InterruptContext()
	t := r.beginTurn(done)
	err := r.agent.Ask(ctx, q, t.Handle)
	t.End(err)
}

func InterruptContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	finished := make(chan struct{})
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
		close(finished)
	}()
	return ctx, func() {
		cancel()
		<-finished
	}
}

func (r *REPL) handleCommand(line string) bool {
	parts := strings.Fields(line)
	switch parts[0] {
	case "/exit", "/quit":
		r.st.out.emit(KindNotice, MsgBye+"\n")
		return true
	case "/help":
		r.st.out.emit(KindNotice, helpText)
	case "/new":
		r.agent.NewSession()
		r.st.out.emit(KindNotice, MsgNewSession)
	case "/load":
		if len(parts) >= 2 {
			if err := r.agent.LoadSession(parts[1]); err != nil {
				r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", err))
			} else {
				r.st.out.emit(KindNotice, fmt.Sprintf(MsgLoadedSess, parts[1]))
				r.warnLegacyPrompt()
			}
			break
		}
		r.loadSessionInteractive()
	case "/context":
		r.st.out.emit(KindNotice, r.agent.ContextInfo()+"\n")
	case "/history":
		r.showHistory(parts[1:])
	case "/model":
		if len(parts) < 2 {
			r.st.out.emit(KindNotice, fmt.Sprintf(MsgCurModel, r.agent.Model()))
			models, err := r.agent.ListModels()
			if err != nil {
				r.st.err.emit(KindError, fmt.Sprintf(MsgModelsFail, err))
				break
			}
			if len(models) == 0 {
				r.st.out.emit(KindNotice, MsgModelsEmpty)
				break
			}
			var b strings.Builder
			b.WriteString(MsgModelsHead)
			for _, m := range models {
				mark := MsgMarkPlain
				if m == r.agent.Model() {
					mark = MsgMarkCurrent
				}
				fmt.Fprintf(&b, "%s%s\n", mark, m)
			}
			r.st.out.emit(KindNotice, b.String())
			break
		}
		r.agent.SetModel(parts[1])
	case "/think":
		r.handleThink(parts[1:])
	case "/md":
		r.mdLive = !r.mdLive
		if r.mdLive {
			r.st.out.emit(KindNotice, MsgMdOn)
		} else {
			r.st.out.emit(KindNotice, MsgMdOff)
		}
	case "/theme":
		r.handleTheme(parts[1:])
	default:
		r.st.err.emit(KindError, MsgUnknownCmd)
	}
	return false
}

func (r *REPL) handleTheme(args []string) {
	if len(args) == 0 {
		var b strings.Builder
		fmt.Fprintf(&b, MsgCurTheme, style.CurrentSchemeName())
		b.WriteString(MsgThemeHead)
		for _, n := range style.SchemeNames() {
			mark := MsgMarkPlain
			if n == style.CurrentSchemeName() {
				mark = MsgMarkCurrent
			}
			if s, ok := style.LookupScheme(n); ok {
				fmt.Fprintf(&b, "%s%s  %s\n", mark, s.Name, s.Desc)
			}
		}
		r.st.out.emit(KindNotice, b.String())
		return
	}
	s, ok := style.ApplyScheme(args[0])
	if !ok {
		bad := fmt.Sprintf(MsgThemeBad, args[0], strings.Join(style.SchemeNames(), "/"))
		r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", bad))
		return
	}
	r.applyTheme(s)
	r.st.out.emit(KindNotice, fmt.Sprintf(MsgThemeSet, s.Name, s.Desc))
	r.printThemeSample()
}

func (r *REPL) printThemeSample() {
	if r.prof.Colors == style.LevelNone {
		return
	}
	line := func(text string) []style.Inline {
		return []style.Inline{style.Span{Text: text}}
	}
	blocks := []style.Block{
		style.Heading{Level: 1, Inlines: line("一级标题")},
		style.Heading{Level: 2, Inlines: line("二级标题")},
		style.Heading{Level: 3, Inlines: line("三级标题")},
		style.Paragraph{Inlines: []style.Inline{style.Span{Text: "正文段落，"}, style.CodeSpan{Text: "行内代码"}, style.Span{Text: "与结尾。"}}},
		style.CodeBlock{Lines: []string{"代码块内容"}},
	}
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(r.rend.Block(blk))
	}
	prompt := r.prompt.Render(func(name string) (string, bool) {
		switch name {
		case "cwd":
			return "~/proj", true
		case "model":
			return "model", true
		case "effort":
			return "high", true
		case "stat":
			return "1.2k", true
		}
		return "", false
	})
	b.WriteString(prompt)
	b.WriteString("\n")
	b.WriteString(style.Dim.Sprint("工具行 ") + style.Info.Sprint("状态行 ") + style.Warn.Sprint("等待中 ") + style.Think.Sprint("思考中 ") + style.Run.Sprint("执行中 ") + style.Ok.Sprint("成功 ") + style.Error.Sprint("错误") + "\n")
	r.st.out.emit(KindNotice, b.String())
}

// applyTheme 把渲染器与提示符切到给定主题（语义色已在 ApplyScheme 中更新）。
func (r *REPL) applyTheme(s style.Scheme) {
	r.promptTpl = s.Prompt
	r.prompt, _ = style.ParseTemplate(s.Prompt)
	r.rend = style.NewThemedRenderer(r.prof, s.MD)
}

func (r *REPL) handleThink(args []string) {
	if len(args) == 0 {
		if cur := r.agent.ReasoningEffort(); cur == "" {
			r.st.out.emit(KindNotice, MsgThinkUnset)
		} else {
			r.st.out.emit(KindNotice, fmt.Sprintf(MsgCurEffort, cur))
		}
		return
	}
	if err := r.agent.SetReasoningEffort(args[0]); err != nil {
		r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", err))
		return
	}
	if cur := r.agent.ReasoningEffort(); cur == "" {
		r.st.out.emit(KindNotice, MsgEffortOff)
	} else {
		r.st.out.emit(KindNotice, fmt.Sprintf(MsgEffortSet, cur))
	}
}

func (r *REPL) showHistory(args []string) {
	msgs := r.agent.History()
	if len(msgs) == 0 {
		r.st.out.emit(KindNotice, MsgNoHistoryMsg)
		return
	}
	if len(args) > 0 {
		if args[0] == "all" {
			for i, m := range msgs {
				if i > 0 {
					r.st.out.emit(KindNotice, "\n")
				}
				r.printHistoryFull(i+1, m)
			}
			return
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 || n > len(msgs) {
			r.st.err.emit(KindError, fmt.Sprintf(MsgInvalidIndex, len(msgs)))
			return
		}
		r.printHistoryFull(n, msgs[n-1])
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, MsgTotalMsgs, len(msgs))
	for i, m := range msgs {
		b.WriteString(historyLine(i+1, m))
		b.WriteString("\n")
	}
	r.st.out.emit(KindNotice, b.String())
}

func historyLabel(m agent.Message) string {
	if m.Role == "tool" && m.Name != "" {
		return m.Name
	}
	return m.Role
}

func historyText(m agent.Message) string {
	if m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) > 0 {
		names := make([]string, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			names[i] = tc.Function.Name
		}
		return fmt.Sprintf(MsgCallLabel, strings.Join(names, ", "))
	}
	return m.Content
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func historyLine(n int, m agent.Message) string {
	text := style.OneLine(historyText(m))
	return fmt.Sprintf("%3d %-9s %s", n, historyLabel(m), truncateRunes(text, 120))
}

func (r *REPL) printHistoryFull(n int, m agent.Message) {
	r.printHistoryHead(n, historyLabel(m))
	if m.Role == "assistant" && m.Content != "" {
		r.printRendered(m.Content)
	} else if text := historyText(m); text != "" {
		if m.Role == "tool" {
			r.st.out.emit(KindToolBlock, style.Dim.Frame(text)+"\n")
		} else {
			r.st.out.emit(KindNotice, text+"\n")
		}
	}
	for _, tc := range m.ToolCalls {
		r.st.out.emit(KindToolBlock, fmt.Sprintf("→ %s %s\n", tc.Function.Name, tc.Function.Arguments))
	}
}

// printHistoryHead 把消息头（#N 角色）按一级标题渲染——`#` 与序号连写不构成 markdown 标题语法，
// 因此不走 markdown 解析，直接构造 Heading IR。
func (r *REPL) printHistoryHead(n int, label string) {
	head := fmt.Sprintf("#%d %s", n, label)
	if !r.mdEnabled() {
		r.st.out.emit(KindContent, head+"\n")
		return
	}
	r.print(r.rend.Block(style.Heading{Level: 1, Inlines: []style.Inline{style.Span{Text: head}}}))
}

// printRendered 把整段文本按与 AI 输出一致的管线渲染（/md 开关 + TTY 旁路），供历史回放等一次性展示使用。
func (r *REPL) printRendered(text string) {
	if !r.mdEnabled() {
		r.st.out.emit(KindContent, text+"\n")
		return
	}
	for _, blk := range mdBlocks(text) {
		r.print(r.rend.Block(blk))
	}
}

func (r *REPL) loadSessionInteractive() {
	list, err := r.agent.ListSessions()
	if err != nil {
		r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", err))
		return
	}
	if len(list) == 0 {
		r.st.out.emit(KindNotice, MsgNoSessions)
		return
	}
	var idx int
	var ok bool
	if r.raw {
		idx, ok = pickSession(r.term, list, r.st.out)
	} else {
		idx, ok = pickByNumber(list, r.st.out)
	}
	if !ok || idx < 0 {
		r.st.out.emit(KindNotice, MsgCancelled)
		return
	}
	if err := r.agent.LoadSession(list[idx].ID); err != nil {
		r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", err))
		return
	}
	r.st.out.emit(KindNotice, fmt.Sprintf(MsgLoadedSess, list[idx].ID))
	r.warnLegacyPrompt()
}

func (r *REPL) warnLegacyPrompt() {
	if r.agent.LegacyPrompt() {
		r.st.out.emit(KindNotice, MsgLegacyHint)
	}
}
