package repl

import (
	"context"
	"errors"
	"fmt"
	"github.com/LaoQi/tanya/render"
	"github.com/LaoQi/tanya/render/ir"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/render/present"
)

const DefaultToolOutputLines = 20

type REPL struct {
	agent         *agent.Agent
	ed            *readline.Editor
	con           readline.Console
	keys          bool
	showReasoning bool
	st            *streams
	prof          term.Profile
	sch           theme.Scheme
	sem           theme.Semantics
	palette       map[string]string
	promptTpl     string
	prompt        render.Template
	view          *toolView
	rend          render.Renderer
	notifier      Notifier
	started       time.Time
	nextPending   bool
	nextInput     string
	imgBytes      int
	imgCount      int
	imgResize     bool
	imgDetail     string
}

type options struct {
	st        *streams
	prof      term.Profile
	views     *present.Registry
	con       readline.Console
	facts     TermFacts
	factsSet  bool
	themeName string
	palette   map[string]string
	reasoning bool
	notifier  Notifier
	maxLines  int
	imgBytes  int
	imgCount  int
	imgResize bool
	imgDetail string
	imgSet    bool
}

func WithImageBehavior(resize bool, detail string) Option {
	return func(o *options) {
		o.imgResize = resize
		o.imgDetail = detail
		o.imgSet = true
	}
}

func WithImageLimits(maxBytes, maxCount int) Option {
	return func(o *options) {
		o.imgBytes = maxBytes
		o.imgCount = maxCount
	}
}

type Option func(*options)

// WithToolViews 注入工具自带视图注册表（装配期一次合成、运行期冻结，nil 即全部走通用回落）。
func WithToolViews(views *present.Registry) Option {
	return func(o *options) { o.views = views }
}

func WithProfile(p term.Profile) Option {
	return func(o *options) { o.prof = p }
}

func WithStreams(st *streams) Option {
	return func(o *options) { o.st = st }
}

func WithConsole(con readline.Console) Option {
	return func(o *options) { o.con = con }
}

func WithShowReasoning(on bool) Option {
	return func(o *options) { o.reasoning = on }
}

func WithNotifier(n Notifier) Option {
	return func(o *options) { o.notifier = n }
}

func WithTermFacts(f TermFacts) Option {
	return func(o *options) { o.facts, o.factsSet = f, true }
}

func WithTheme(name string, palette map[string]string) Option {
	return func(o *options) { o.themeName, o.palette = name, palette }
}

func WithToolOutputLines(n int) Option {
	return func(o *options) { o.maxLines = n }
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
	con := o.con
	if con == nil {
		con = readline.NewConsole()
	}
	keys := con.BeginRead() == nil
	if keys {
		con.EndRead()
	}
	ed := readline.NewEditor(con)
	ed.SetOutput(o.st.out)
	c := &completer{listSessions: a.ListSessions, listModels: a.ListModels, workspaceDir: a.Workspace}
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
	imgResize := o.imgResize
	if !o.imgSet {
		imgResize = true
	}
	imgBytes, imgCount := o.imgBytes, o.imgCount
	if imgBytes <= 0 {
		imgBytes = agent.DefaultImageMaxBytes
	}
	if imgCount <= 0 {
		imgCount = agent.DefaultImageMaxCount
	}
	r := &REPL{agent: a, ed: ed, con: con, keys: keys, showReasoning: o.reasoning, notifier: o.notifier, st: o.st, promptTpl: promptTpl, prompt: tpl, sch: sch, sem: sem, palette: o.palette, imgBytes: imgBytes, imgCount: imgCount, imgResize: imgResize, imgDetail: o.imgDetail}
	r.prof = o.prof
	r.rend = render.NewThemedRenderer(r.prof, sch.MD)
	ed.SetStyles(sem.Dim.With(r.prof), sem.Accent.With(r.prof))
	maxLines := o.maxLines
	if maxLines < 1 {
		maxLines = DefaultToolOutputLines
	}
	facts := o.facts
	if !o.factsSet {
		if s, ok := con.Size(); ok && s.Cols > 0 {
			facts = TermFacts{Cols: s.Cols, ColsOK: true}
		}
	}
	r.view = NewToolView(o.st, r.prof, r.sem, LiveWidth(con, facts), maxLines, o.views)
	return r, nil
}

func (r *REPL) print(text string, kind Kind) {
	r.view.Content(kind, text)
}

// failErr/failText 是标准错误行的唯一动词：错误信息可能内嵌服务端返回体、路径等外部内容，
// 一律经 errLine/errLineText 清洗，不再出现裸 fmt.Sprintf(MsgErrLineFmt...) 样板。
func (r *REPL) failErr(err error) {
	r.st.err.emit(KindError, errLine(err))
}

func (r *REPL) failText(s string) {
	r.st.err.emit(KindError, errLineText(s))
}

// notify 是注意力通知的唯一出口：门禁（并非交互富档 TTY 会话、未装配行为）之外一律静默，
// 行为本身由 Notifier 决定，不在这里判 TTY 之外的平台细节。
func (r *REPL) notify(n Notification) {
	if r.notifier == nil || !r.prof.TTY || !r.st.decor() {
		return
	}
	if payload, ok := payloadOf(n); ok {
		r.notifier.Notify(payload)
	}
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
	return "\n" + sem.Ok.With(prof).Sprint(text) + "\n"
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
	return r.sem.Warn.With(r.prof).Sprint(MsgNoSaveWarn) + "\n"
}

func (r *REPL) Run() error {
	r.started = time.Now()
	r.st.out.emit(KindDecor, welcomeText()+r.noSaveWarn())
	r.autoArchivePrompt()
	for {
		prompt := r.prompt.Render(r.prof, r.resolveVars())
		line, err := r.ed.Readline(prompt)
		if err == readline.ErrInterrupt {
			continue
		}
		if err == io.EOF || errors.Is(err, readline.ErrExited) {
			return r.quit()
		}
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isExitLine(line) {
			return r.quit()
		}
		if isSlashCommand(line) {
			if r.handleCommand(line) {
				return r.quit()
			}
			r.st.out.emit(KindDecor, turnSep(r.prof, r.sem, 0))
			continue
		}
		if text, ok := dialogueText(line); ok {
			if text == "" {
				r.st.out.emit(KindNotice, MsgDialogueEmpty)
				continue
			}
			if r.blockArchiveReadOnly() {
				continue
			}
			r.ask(text)
			continue
		}
		if r.blockArchiveReadOnly() {
			continue
		}
		r.ask(line)
	}
}

func (r *REPL) attachOptions() AttachOptions {
	return AttachOptions{
		Workspace: r.cwdBase,
		MaxBytes:  r.imgBytes,
		MaxCount:  r.imgCount,
		Resize:    r.imgResize,
		Detail:    r.imgDetail,
	}
}

func (r *REPL) cwdBase() string {
	if r.agent != nil {
		if ws := r.agent.Workspace(); ws != "" {
			return ws
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func (r *REPL) ask(q string) {
	in, err := PrepareContent(q, r.attachOptions())
	if err != nil {
		r.failErr(err)
		return
	}
	if len(in.Images) > 0 {
		r.st.out.emit(KindDecor, r.sem.Dim.With(r.prof).Frame(imagesText(in.Images))+"\n")
	}
	r.runPrompt(in, false)
}

func (r *REPL) continueTurn() { r.runPrompt(agent.Content{}, true) }

func (r *REPL) runPrompt(in agent.Content, cont bool) {
	r.con.Sane()
	ctx, done := InterruptContext(r.con)
	t := r.beginTurn(done)
	err := r.runTurn(ctx, in, t, cont)
	t.End(err)
	r.afterTurn()
}

func (r *REPL) afterTurn() {
	if r.agent == nil {
		return
	}
	h, ok := r.agent.TakeHandoff()
	if !ok {
		return
	}
	fromNext := r.nextPending
	input := ""
	if fromNext {
		r.nextPending, input = false, r.nextInput
		r.nextInput = ""
	}
	r.st.out.emit(KindDecor, handoffText(farewellInfo{
		session:  h.OldID,
		duration: time.Since(r.started),
		stats:    h.OldStats,
		file:     h.OldFile,
		noSave:   h.NoSave,
	}, h))
	r.started = time.Now()
	r.st.out.emit(KindDecor, turnSep(r.prof, r.sem, 0))
	switch {
	case input != "":
		r.ask(input)
	case fromNext:
	case h.Continue:
		r.continueTurn()
	}
}

func handoffText(info farewellInfo, h agent.Handoff) string {
	return farewellText(info) + fmt.Sprintf(MsgHandoffBlockFmt, h.NewID, strings.Count(h.Summary, "\n")+1)
}

func (r *REPL) handleNext(args []string) {
	if r.agent == nil {
		return
	}
	if id, ok := r.agent.ArchiveReadOnly(); ok {
		r.st.out.emit(KindNotice, fmt.Sprintf(MsgHandoffReadOnlyFmt, id))
		return
	}
	r.nextInput = strings.TrimSpace(strings.Join(args, " "))
	r.nextPending = true
	r.ask(agent.MsgHandoffPrompt)
	if r.nextPending {
		r.nextPending, r.nextInput = false, ""
		r.st.out.emit(KindNotice, MsgHandoffNone)
	}
}

func InterruptContext(con readline.Console) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	unsub := con.Subscribe(func(ev readline.Event) {
		if ev.Kind == readline.EventInterrupt {
			cancel()
		}
	})
	finished := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(finished)
	}()
	return ctx, func() {
		cancel()
		unsub()
		<-finished
	}
}

func (r *REPL) handleCommand(line string) bool {
	parts := strings.Fields(line)
	switch parts[0] {
	case "/exit", "/quit":
		return true
	case "/help":
		r.st.out.emit(KindNotice, helpText)
	case "/new":
		r.agent.NewSession()
		r.st.out.emit(KindNotice, MsgNewSession)
	case "/switch":
		r.handleSwitch(parts[1:])
	case "/load":
		if len(parts) >= 2 {
			if err := r.agent.LoadSession(parts[1]); err != nil {
				r.failErr(err)
			} else {
				r.st.out.emit(KindNotice, r.loadNotice(parts[1]))
				r.warnLegacyPrompt()
			}
			break
		}
		r.loadSessionInteractive()
	case "/archive":
		r.handleArchive(parts[1:])
	case "/fork":
		r.handleFork()
	case "/next":
		r.handleNext(parts[1:])
	case "/stat":
		r.st.out.emitText(KindNotice, statInfo(r.agent.Stats())+"\n")
	case "/history":
		r.showHistory(parts[1:])
	case "/model":
		if len(parts) < 2 {
			r.st.out.emitText(KindNotice, fmt.Sprintf(MsgCurModel, r.agent.Model()))
			models, err := r.agent.ListModels()
			if err != nil {
				r.st.err.emitText(KindError, fmt.Sprintf(MsgModelsFail, err))
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
				fmt.Fprintf(&b, "%s%s\n", mark, term.OneLine(m))
			}
			r.st.out.emit(KindNotice, b.String())
			break
		}
		if err := r.agent.SetModel(parts[1]); err != nil {
			r.failErr(err)
		}
	case "/think":
		r.handleThink(parts[1:])
	case "/reasoning":
		r.handleReasoning(parts[1:])
	case "/theme":
		r.handleTheme(parts[1:])
	}
	return false
}

func (r *REPL) blockArchiveReadOnly() bool {
	if r.agent == nil {
		return false
	}
	id, ok := r.agent.ArchiveReadOnly()
	if !ok {
		return false
	}
	r.st.out.emit(KindNotice, fmt.Sprintf(MsgArchiveReadOnlyFmt, id))
	return true
}

func (r *REPL) loadNotice(id string) string {
	if aid, ok := r.agent.ArchiveReadOnly(); ok && aid == id {
		return fmt.Sprintf(MsgLoadArchived, id)
	}
	return fmt.Sprintf(MsgLoadedSess, id)
}

func (r *REPL) archiveInteractive() bool {
	return r.agent != nil && r.keys && r.prof.TTY && !r.st.mode.plain()
}

func (r *REPL) readConfirm(prompt string) (string, error) {
	r.ed.SetHistoryFilter(func(string) bool { return false })
	defer r.ed.SetHistoryFilter(nil)
	return r.ed.Readline(prompt)
}

func (r *REPL) handleArchive(args []string) {
	if !r.archiveInteractive() {
		r.st.out.emit(KindNotice, MsgArchiveOnlyTTY)
		return
	}
	arg := strings.TrimSpace(strings.Join(args, " "))
	opt, err := ParseArchiveArg(arg, r.agent.AutoArchiveKeep())
	if err != nil {
		r.failErr(err)
		return
	}
	r.archiveFlow(opt, arg)
}

func (r *REPL) handleFork() {
	if r.agent == nil {
		return
	}
	inherited := len(r.agent.History())
	id, err := r.agent.Fork()
	if err != nil {
		r.failErr(err)
		return
	}
	out := fmt.Sprintf(MsgForkDone, id, inherited)
	if r.agent.NoSave() {
		out += MsgForkNoSave
	}
	r.st.out.emit(KindNotice, out)
}

func (r *REPL) handleSwitch(args []string) {
	if r.agent == nil {
		return
	}
	from := r.agent.Workspace()
	if len(args) == 0 {
		r.st.out.emitText(KindNotice, fmt.Sprintf(MsgSwitchUsage, initPath(from)))
		return
	}
	retired := r.farewellData()
	if err := r.agent.SwitchWorkspace(strings.Join(args, " ")); err != nil {
		r.failErr(err)
		return
	}
	to := r.agent.Workspace()
	out := fmt.Sprintf(MsgSwitchDone, initPath(from), initPath(to))
	if dir, ok := r.agent.SessionDir(); ok {
		out += fmt.Sprintf(MsgSwitchDir, initPath(dir))
	}
	r.st.out.emit(KindDecor, farewellText(retired))
	r.st.out.emitText(KindNotice, out)
	r.started = time.Now()
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
		r.failText(bad)
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
		ir.Table{
			Aligns: []ir.Align{ir.AlignLeft, ir.AlignRight},
			Widths: []int{6, 4},
			Rows: []ir.TableRow{
				{Cells: [][]ir.Inline{line("表格头"), line("数值")}, Header: true},
				{Cells: [][]ir.Inline{line("示例行"), line("42")}},
			},
			Top:    true,
			Bottom: true,
		},
	}
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(r.rend.Block(blk))
	}
	prompt := r.prompt.Render(r.prof, func(name string) (string, bool) {
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
	p := r.prof
	b.WriteString(r.sem.Dim.With(p).Sprint("工具行 ") + r.sem.Info.With(p).Sprint("状态行 ") + r.sem.Warn.With(p).Sprint("等待中 ") + r.sem.Think.With(p).Sprint("思考中 ") + r.sem.Run.With(p).Sprint("执行中 ") + r.sem.Ok.With(p).Sprint("成功 ") + r.sem.Error.With(p).Sprint("错误") + "\n")
	r.st.out.emit(KindNotice, b.String())
}

// applyTheme 把语义色、渲染器与提示符切到给定主题（palette 覆盖重放）。
func (r *REPL) applyTheme(s theme.Scheme) {
	r.sch = s
	r.sem = theme.Apply(s.Sem, r.palette)
	r.promptTpl = s.Prompt
	r.prompt, _ = render.ParseTemplate(s.Prompt, r.sem)
	r.rend = render.NewThemedRenderer(r.prof, s.MD)
	r.ed.SetStyles(r.sem.Dim.With(r.prof), r.sem.Accent.With(r.prof))
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
		r.failErr(err)
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
	if len(m.Images) > 0 {
		if text := strings.TrimSpace(m.Content); text != "" {
			return imagesText(m.Images) + " " + text
		}
		return imagesText(m.Images)
	}
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
			r.st.out.emit(KindToolBlock, r.sem.Dim.With(r.prof).Frame(text)+"\n")
		} else {
			r.st.out.emitText(KindNotice, text+"\n")
		}
	}
	for _, tc := range m.ToolCalls {
		r.st.out.emitText(KindToolBlock, fmt.Sprintf("→ %s %s\n", tc.Function.Name, tc.Function.Arguments))
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
		r.st.out.emitText(KindContent, text+"\n")
		return
	}
	for _, blk := range mdBlocks(text, r.view.width()) {
		r.print(r.rend.Block(blk), KindContent)
	}
}

func (r *REPL) loadSessionInteractive() {
	list, err := r.agent.ListSessions()
	if err != nil {
		r.failErr(err)
		return
	}
	if len(list) == 0 {
		r.st.out.emit(KindNotice, MsgNoSessions)
		return
	}
	idx, ok, keys := pickSession(r.con, list, r.st.out, r.sem, r.prof)
	if !keys {
		idx, ok = pickByNumber(list, r.st.out)
	}
	if !ok || idx < 0 {
		r.st.out.emit(KindNotice, MsgCancelled)
		return
	}
	if err := r.agent.LoadSession(list[idx].ID); err != nil {
		r.failErr(err)
		return
	}
	r.st.out.emit(KindNotice, r.loadNotice(list[idx].ID))
	r.warnLegacyPrompt()
}

func (r *REPL) warnLegacyPrompt() {
	if r.agent.LegacyPrompt() {
		r.st.out.emit(KindNotice, MsgLegacyHint)
	}
}
