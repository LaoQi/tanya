package repl

import (
	"context"
	"fmt"
	"github.com/LaoQi/tanya/render"
	"github.com/LaoQi/tanya/render/ir"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
)

type REPL struct {
	agent     *agent.Agent
	ed        *readline.Editor
	term      readline.Terminal
	raw       bool
	st        *streams
	prof      term.Profile
	sch       theme.Scheme
	sem       theme.Semantics
	palette   map[string]string
	promptTpl string
	prompt    render.Template
	view      *toolView
	rend      render.Renderer
}

type options struct {
	st        *streams
	term      readline.Terminal
	raw       bool
	facts     TermFacts
	factsSet  bool
	themeName string
	palette   map[string]string
}

type Option func(*options)

func WithStreams(st *streams) Option {
	return func(o *options) { o.st = st }
}

func WithTerminal(dev readline.Terminal, raw bool) Option {
	return func(o *options) { o.term, o.raw = dev, raw }
}

func WithTermFacts(f TermFacts) Option {
	return func(o *options) { o.facts, o.factsSet = f, true }
}

func WithTheme(name string, palette map[string]string) Option {
	return func(o *options) { o.themeName, o.palette = name, palette }
}

// Semantics 按主题名与 palette 覆盖计算语义色集合；主题名非法时回落 default。
func Semantics(name string, palette map[string]string) theme.Semantics {
	sch, ok := theme.Lookup(name)
	if !ok {
		sch, _ = theme.Lookup("default")
	}
	return theme.Apply(sch.Sem, palette)
}

// ValidateTheme 校验主题名（配置校验归表现层，agent 不依赖 theme）。
func ValidateTheme(name string) error {
	if !theme.Has(name) {
		return fmt.Errorf(MsgBadTheme, name, strings.Join(theme.Names(), "/"))
	}
	return nil
}

func NewREPL(a *agent.Agent, promptTpl string, opts ...Option) (*REPL, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.st == nil {
		o.st = NewStreams(os.Stdout, os.Stderr, modeRich)
	}
	dev, raw := o.term, o.raw
	if dev == nil {
		dev, raw = readline.NewTerminal()
	}
	ed := readline.NewEditor(dev, raw)
	ed.SetOutput(o.st.out)
	c := &completer{listSessions: a.ListSessions, listModels: a.ListModels}
	ed.SetComplete(c.complete)
	ed.SetGhost(c.suggest)
	name := o.themeName
	if name == "" {
		name = "default"
	}
	sch, ok := theme.Lookup(name)
	if !ok {
		sch, _ = theme.Lookup("default")
	}
	sem := theme.Apply(sch.Sem, o.palette)
	if promptTpl == "" {
		promptTpl = sch.Prompt
	}
	tpl, err := render.ParseTemplate(promptTpl, sem)
	if err != nil {
		return nil, err
	}
	r := &REPL{agent: a, ed: ed, term: dev, raw: raw, st: o.st, promptTpl: promptTpl, prompt: tpl, sch: sch, sem: sem, palette: o.palette}
	r.prof = term.GetProfile()
	r.rend = render.NewThemedRenderer(r.prof, sch.MD)
	ed.SetStyles(sem.Dim, sem.Accent)
	maxLines := 20
	if a != nil {
		maxLines = a.ToolOutputLines()
	}
	facts := o.facts
	if !o.factsSet {
		if s, ok := dev.Size(); ok && s.Cols > 0 {
			facts = TermFacts{Cols: s.Cols, ColsOK: true}
		}
	}
	r.view = NewToolView(o.st, r.prof, r.sem, facts.Width, maxLines)
	return r, nil
}

func (r *REPL) print(text string, kind Kind) {
	r.view.Content(kind, text)
}

func (r *REPL) mdEnabled() bool {
	return r.prof.TTY && r.st.decor()
}

func turnSep(prof term.Profile, sem theme.Semantics, d time.Duration) string {
	if !prof.TTY {
		return ""
	}
	text := fmt.Sprintf(TurnSepTimeFmt, time.Now().Format("15:04:05"))
	if d > 0 {
		text += fmt.Sprintf(TurnSepDurFmt, turnDuration(d))
	}
	return "\n" + sem.Ok.Sprint(text) + "\n"
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
	var st agent.Stats
	var loaded bool
	load := func() agent.Stats {
		if !loaded {
			st = r.agent.Stats()
			loaded = true
		}
		return st
	}
	return func(name string) (string, bool) {
		switch name {
		case "cwd":
			return r.cwdLabel(), true
		case "model":
			return r.agent.Model(), true
		case "effort":
			return r.agent.ReasoningEffort(), true
		case "usage":
			return usageText(load()), true
		case "cache":
			return cacheText(load()), true
		case "cache_rate":
			return cacheRateText(load()), true
		case "usage_total":
			return usageTotalText(load()), true
		case "cache_total":
			return cacheTotalText(load()), true
		case "cache_rate_total":
			return cacheRateTotalText(load()), true
		case "usage_summary":
			return summaryText(load()), true
		}
		return "", false
	}
}

func (r *REPL) Close() {}

func (r *REPL) noSaveWarn() string {
	if r.agent == nil || !r.agent.NoSave() {
		return ""
	}
	return r.sem.Warn.Sprint(MsgNoSaveWarn) + "\n"
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
			r.st.out.emit(KindDecor, turnSep(r.prof, r.sem, 0))
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
	case "/stat":
		r.st.out.emit(KindNotice, statInfo(r.agent.Stats())+"\n")
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
		if err := r.agent.SetModel(parts[1]); err != nil {
			r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", err))
		}
	case "/think":
		r.handleThink(parts[1:])
	case "/theme":
		r.handleTheme(parts[1:])
	}
	return false
}

func (r *REPL) handleTheme(args []string) {
	if len(args) == 0 {
		var b strings.Builder
		fmt.Fprintf(&b, MsgCurTheme, r.sch.Name)
		b.WriteString(MsgThemeHead)
		for _, n := range theme.Names() {
			mark := MsgMarkPlain
			if n == r.sch.Name {
				mark = MsgMarkCurrent
			}
			if s, ok := theme.Lookup(n); ok {
				fmt.Fprintf(&b, "%s%s  %s\n", mark, s.Name, s.Desc)
			}
		}
		r.st.out.emit(KindNotice, b.String())
		return
	}
	s, ok := theme.Lookup(args[0])
	if !ok {
		bad := fmt.Sprintf(MsgBadTheme, args[0], strings.Join(theme.Names(), "/"))
		r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", bad))
		return
	}
	r.applyTheme(s)
	r.st.out.emit(KindNotice, fmt.Sprintf(MsgThemeSet, s.Name, s.Desc))
	r.printThemeSample()
}

func (r *REPL) printThemeSample() {
	if r.prof.Colors == term.LevelNone {
		return
	}
	line := func(text string) []ir.Inline {
		return []ir.Inline{ir.Span{Text: text}}
	}
	blocks := []ir.Block{
		ir.Heading{Level: 1, Inlines: line("一级标题")},
		ir.Heading{Level: 2, Inlines: line("二级标题")},
		ir.Heading{Level: 3, Inlines: line("三级标题")},
		ir.Paragraph{Inlines: []ir.Inline{ir.Span{Text: "正文段落，"}, ir.CodeSpan{Text: "行内代码"}, ir.Span{Text: "与结尾。"}}},
		ir.CodeBlock{Lines: []string{"代码块内容"}},
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
		case "usage_summary":
			return "1.2k", true
		}
		return "", false
	})
	b.WriteString(prompt)
	b.WriteString("\n")
	b.WriteString(r.sem.Dim.Sprint("工具行 ") + r.sem.Info.Sprint("状态行 ") + r.sem.Warn.Sprint("等待中 ") + r.sem.Think.Sprint("思考中 ") + r.sem.Run.Sprint("执行中 ") + r.sem.Ok.Sprint("成功 ") + r.sem.Error.Sprint("错误") + "\n")
	r.st.out.emit(KindNotice, b.String())
}

// applyTheme 把语义色、渲染器与提示符切到给定主题（palette 覆盖重放）。
func (r *REPL) applyTheme(s theme.Scheme) {
	r.sch = s
	r.sem = theme.Apply(s.Sem, r.palette)
	r.promptTpl = s.Prompt
	r.prompt, _ = render.ParseTemplate(s.Prompt, r.sem)
	r.rend = render.NewThemedRenderer(r.prof, s.MD)
	r.ed.SetStyles(r.sem.Dim, r.sem.Accent)
	r.view.setSemantics(r.sem)
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
	text := term.OneLine(historyText(m))
	return fmt.Sprintf("%3d %-9s %s", n, historyLabel(m), truncateRunes(text, 120))
}

func (r *REPL) printHistoryFull(n int, m agent.Message) {
	r.printHistoryHead(n, m)
	if m.Role == "assistant" && m.Content != "" {
		r.printRendered(m.Content)
	} else if text := historyText(m); text != "" {
		if m.Role == "tool" {
			r.st.out.emit(KindToolBlock, r.sem.Dim.Frame(text)+"\n")
		} else {
			r.st.out.emit(KindNotice, text+"\n")
		}
	}
	for _, tc := range m.ToolCalls {
		r.st.out.emit(KindToolBlock, fmt.Sprintf("→ %s %s\n", tc.Function.Name, tc.Function.Arguments))
	}
}

// printHistoryHead 把消息头（#N 角色）按一级标题渲染——`#` 与序号连写不构成 markdown 标题语法，
// 因此不走 markdown 解析，直接构造 ir.Heading IR。
func (r *REPL) printHistoryHead(n int, m agent.Message) {
	head := fmt.Sprintf("#%d %s", n, historyLabel(m))
	if !r.mdEnabled() {
		r.st.out.emit(KindNotice, head+"\n")
		return
	}
	st := r.sem.Warn
	if m.Role == "user" {
		st = r.sem.Ok
	}
	r.print(r.rend.Block(ir.Heading{Level: 1, Inlines: []ir.Inline{ir.Span{Style: st, Text: head}}}), KindNotice)
}

// printRendered 把整段文本按与 AI 输出一致的管线渲染（TTY + rich 走渲染，其余旁路），供历史回放等一次性展示使用。
func (r *REPL) printRendered(text string) {
	if !r.mdEnabled() {
		r.st.out.emit(KindContent, text+"\n")
		return
	}
	for _, blk := range mdBlocks(text) {
		r.print(r.rend.Block(blk), KindContent)
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
		idx, ok = pickSession(r.term, list, r.st.out, r.sem)
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
