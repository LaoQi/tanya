package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/LaoQi/tanya/render/present"
	"github.com/LaoQi/tanya/render/term"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/attach"
	"github.com/LaoQi/tanya/config"
	"github.com/LaoQi/tanya/ctty"
	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/repl"
	"github.com/LaoQi/tanya/tools"
	"github.com/LaoQi/tanya/tools/shell"
	shellview "github.com/LaoQi/tanya/tools/shell/view"
)

var (
	version   = "dev"
	buildTime = ""
)

const envAllowRoot = "TANYA_ALLOW_ROOT"

//go:embed system_prompt.md
var systemPromptFile string

//go:embed config.example.yaml
var configExampleFile string

type cliFlags struct {
	showVersion *bool
	configPath  *string
	sessionMode *string
	model       *string
	noSave      *bool
	plain       *bool
	verbose     *bool
}

// registerFlags 登记全部命令行选项（main 与用法测试共用同一份定义）。
func registerFlags(fs *flag.FlagSet) *cliFlags {
	f := &cliFlags{}
	f.showVersion = fs.Bool("v", false, repl.FlagVersion)
	f.configPath = fs.String("c", "", repl.FlagConfig)
	f.sessionMode = fs.String("m", "", repl.FlagMode)
	f.model = fs.String("model", "", repl.FlagModel)
	f.noSave = fs.Bool("n", false, repl.FlagNoSave)
	fs.BoolVar(f.noSave, "no-save", false, repl.FlagNoSave)
	f.plain = fs.Bool("p", false, repl.FlagPlain)
	fs.BoolVar(f.plain, "plain", false, repl.FlagPlain)
	f.verbose = fs.Bool("verbose", false, repl.FlagVerbose)
	return f
}

func main() {
	f := registerFlags(flag.CommandLine)
	flag.Usage = func() { writeUsage(os.Stderr, flag.CommandLine) }
	flag.Parse()
	ctty.EnsureUTF8()
	defer ctty.RestoreUTF8()

	cmd, rest := repl.ParseCommand(flag.Args())
	mode, err := repl.ParseMode(*f.plain, *f.verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, repl.MsgErrLineFmt+"\n", err)
		exitNow(1)
	}
	if cmd == repl.CmdAsk {
		mode = repl.SingleShot(mode)
	}
	st := repl.NewStreams(os.Stdout, os.Stderr, mode)

	if err := rootRefusal(); err != nil {
		st.FailErr("", err)
		exitNow(1)
	}

	if *f.showVersion {
		st.Print(fmt.Sprintf("tanya %s\n", version))
		return
	}
	if cmd == repl.CmdConfig {
		if rest != "" {
			st.Fail(repl.MsgConfigUsage)
			exitNow(1)
		}
		repl.RunConfig(st, configExampleFile)
		return
	}
	if cmd == repl.CmdAsk && rest == "" {
		st.Fail(repl.MsgAskUsage)
		exitNow(1)
	}
	if cmd == repl.CmdInit && rest != "" {
		st.Fail(repl.MsgInitUsage)
		exitNow(1)
	}

	repl.Version = version
	repl.BuildTime = buildTime
	config.Version = version

	cfg, err := config.Load(*f.configPath)
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	if err := repl.ValidateTheme(cfg.Theme); err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	sem := repl.Semantics(cfg.Theme, cfg.Palette)
	facts := ctty.Probe()
	prof := term.DetectProfile(facts.StdoutTTY, facts.VT)
	switch cfg.Colors {
	case "on":
		if prof.Colors == term.LevelNone {
			prof.Colors = term.Level16
		}
	case "off":
		prof.Colors = term.LevelNone
	}
	if *f.plain {
		prof.Colors = term.LevelNone
	}
	if *f.sessionMode != "" {
		cfg.SessionMode = *f.sessionMode
	}
	applyModel(cfg, *f.model)
	var takeover *attach.Result
	if cmd == repl.CmdAttach {
		if *f.noSave {
			st.FailErr("", errors.New(repl.MsgAttachNoSave))
			exitNow(1)
		}
		args, err := repl.ParseAttachArgs(rest)
		if err != nil {
			st.FailErr("", err)
			exitNow(1)
		}
		res, listed, err := runAttach(cfg, args, st)
		if err != nil {
			st.FailErr("", err)
			exitNow(1)
		}
		if listed {
			return
		}
		takeover = res
	}
	if cmd == repl.CmdInit {
		if err := repl.RunInit(st, sem, prof, &cfg.Config); err != nil {
			st.FailErr("", err)
			exitNow(1)
		}
	}
	shell.ProtectTerminalSignals()
	ctty.WatchSignals()
	inv, err := shell.Resolve(cfg.Shell)
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	home, _ := os.UserHomeDir()
	var a *agent.Agent
	con := readline.NewConsole()
	list, err := tools.Standard(tools.Options{
		ShellOverride: cfg.Shell,
		Home:          home,
		Workspace:     func() string { return a.Workspace() },
		Console:       consoleForShell{con},
		ImageMaxBytes: cfg.ImageMaxBytes,
		ImageResize:   cfg.ImageResize,
		ImageDetail:   cfg.ImageDetail,
	})
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	a, err = agent.New(&cfg.Config,
		agent.NoSave(*f.noSave),
		agent.WithTools(list...),
		agent.WithSystemPrompt(systemBase(inv)),
		agent.WithAttachOps(attachOps{runDir: func() (string, bool) { return a.RunDir() }}))
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	if takeover != nil {
		if err := a.AdoptSession(takeover.File); err != nil {
			st.FailErr("", err)
			exitNow(1)
		}
		if takeover.Model != "" {
			_ = a.SetModel(takeover.Model)
		}
		if takeover.Effort != "" {
			_ = a.SetReasoningEffort(takeover.Effort)
		}
	}
	termFacts := repl.TermFacts{Cols: facts.Cols, ColsOK: facts.SizeOK}
	views := present.NewRegistry()
	views.Register("run_shell", shellview.View())
	sink := repl.NewToolView(st, prof, sem, repl.LiveWidth(con, termFacts), cfg.ToolOutputLines, views)

	if cmd == repl.CmdAsk {
		ctx, done := repl.InterruptContext(con)
		err := a.Ask(ctx, rest, sink.Handle)
		done()
		if err != nil {
			if ctty.Exiting() {
				exitNow(ctty.ExitStatus())
			}
			st.FailErr("\n", err)
			exitNow(1)
		}
		st.End()
		if code := ctty.ExitStatus(); code != 0 {
			exitNow(code)
		}
		return
	}

	notifier, err := buildNotifier(cfg.UI, inv)
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	opts := []repl.Option{repl.WithStreams(st), repl.WithProfile(prof), repl.WithToolViews(views), repl.WithTermFacts(termFacts), repl.WithTheme(cfg.Theme, cfg.Palette), repl.WithShowReasoning(cfg.ShowReasoning), repl.WithNotifier(notifier), repl.WithToolOutputLines(cfg.ToolOutputLines), repl.WithConsole(con)}
	if takeover != nil {
		opts = append(opts, repl.WithTakeover(*takeover))
	}
	r, err := repl.NewREPL(a, "", opts...)
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	defer r.Close()
	if err := r.Run(); err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	if code := ctty.ExitStatus(); code != 0 {
		exitNow(code)
	}
}

// printer 是 main 需要的输出面（repl.streams 的 Print）。
type printer interface{ Print(text string) }

// runAttach 在当前工作区发现目标实例并请求接管；listed 为真表示只是列出（不接管）。
func runAttach(cfg *config.Config, args repl.AttachArgs, st printer) (*attach.Result, bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, false, err
	}
	dir := agent.RunDirFor(&cfg.Config, cwd)
	if args.Prune {
		rep, err := attach.Prune(dir)
		if err != nil {
			return nil, false, err
		}
		printAttachPrune(st, rep)
		return nil, true, nil
	}
	rows, err := attach.Snapshot(dir)
	if err != nil {
		return nil, false, err
	}
	alive := make([]attach.Entry, 0, len(rows))
	for _, r := range rows {
		if r.Alive {
			alive = append(alive, r.Entry)
		}
	}
	if args.List {
		printAttachList(st, dir, rows)
		return nil, true, nil
	}
	var target attach.Entry
	switch {
	case args.PID > 0:
		found := false
		for _, e := range alive {
			if e.PID == args.PID {
				target, found = e, true
				break
			}
		}
		if !found {
			for _, r := range rows {
				if r.Entry.PID == args.PID {
					return nil, false, fmt.Errorf(repl.MsgAttachStaleFmt, args.PID)
				}
			}
			return nil, false, fmt.Errorf(repl.MsgAttachNoPIDFmt, args.PID)
		}
	case len(alive) == 1:
		target = alive[0]
	case len(alive) == 0:
		return nil, false, errors.New(repl.MsgAttachNone)
	default:
		printAttachList(st, dir, rows)
		return nil, false, fmt.Errorf(repl.MsgAttachManyFmt, len(alive))
	}
	res, err := takeOver(target, args.Force, st)
	if err != nil {
		return nil, false, err
	}
	if !res.OK {
		return nil, false, fmt.Errorf(repl.MsgAttachRefusedFmt, attachRefused(res.Code))
	}
	if res.Protocol != cfg.ApiProtocol {
		return nil, false, fmt.Errorf(repl.MsgAttachProtocolFmt, res.Protocol, cfg.ApiProtocol)
	}
	if res.File == "" {
		return nil, false, errors.New(repl.MsgAttachNoSessionFmt)
	}
	return &res, false, nil
}

// attachWaitRefresh 是等待接管期间刷新进度行的周期（var 便于测试缩短）。
var attachWaitRefresh = 5 * time.Second

// takeOver 发起接管并等待结果：目标忙碌时按本地注册项刷新进度，^C 取消等待（关闭连接）。
func takeOver(target attach.Entry, force bool, st printer) (attach.Result, error) {
	c, err := attach.Open(target)
	if err != nil {
		return attach.Result{}, err
	}
	defer c.Close()
	ctty.WatchSignals()
	done := make(chan struct{})
	var res attach.Result
	var terr error
	go func() {
		defer close(done)
		res, terr = c.Takeover(force)
	}()
	var tick <-chan time.Time
	if !force {
		if target.State == attach.StateBusy {
			st.Print(repl.MsgAttachWaiting)
		}
		t := time.NewTicker(attachWaitRefresh)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case <-done:
			return res, terr
		case <-tick:
			e, err := c.State()
			if err != nil || e.State != attach.StateBusy || e.TurnStarted.IsZero() {
				continue
			}
			st.Print(fmt.Sprintf(repl.MsgAttachWaitingFmt, shortDur(time.Since(e.TurnStarted))))
		case <-ctty.Interrupted():
			st.Print(repl.MsgAttachCanceled)
			c.Close()
			<-done
			return attach.Result{}, errors.New(repl.MsgAttachCanceled)
		}
	}
}

func attachRefused(code string) string {
	switch code {
	case attach.CodeNoSave:
		return "对方处于不落盘模式"
	case attach.CodeReadOnly:
		return "对方是归档只读会话，请在其上 /fork"
	case attach.CodeInProgress:
		return "对方已有接管请求在处理"
	case attach.CodeNoSession:
		return "对方没有可接管的会话"
	case attach.CodeIOError:
		return "对方写入会话失败，接管未成立"
	case attach.CodeVersion:
		return "协议版本不匹配"
	case attach.CodeBadRequest:
		return "对方输入设备不支持接管注入"
	}
	return code
}

// attachOps 把 attach 发现域能力交给 agent_custom（agent 侧不感知 attach）；
// 工作区经闭包活取，/switch 后自动跟随。
type attachOps struct {
	runDir func() (string, bool)
}

func (o attachOps) dir() (string, error) {
	dir, ok := o.runDir()
	if !ok || dir == "" {
		return "", errors.New(repl.MsgAttachPruneNoSave)
	}
	return dir, nil
}

func (o attachOps) List() (string, error) {
	dir, err := o.dir()
	if err != nil {
		return "", err
	}
	rows, err := attach.Snapshot(dir)
	if err != nil {
		return "", err
	}
	return attachListText(dir, rows), nil
}

func (o attachOps) Prune() (string, error) {
	dir, err := o.dir()
	if err != nil {
		return "", err
	}
	rep, err := attach.Prune(dir)
	if err != nil {
		return "", err
	}
	return attachPruneText(rep), nil
}

func printAttachList(st printer, dir string, rows []attach.Row) {
	st.Print(attachListText(dir, rows))
}

// attachListText 渲染运行实例列表（CLI --list 与 agent_custom get attach 共用同一份文本）。
func attachListText(dir string, rows []attach.Row) string {
	var b strings.Builder
	fmt.Fprintf(&b, repl.MsgAttachListHead, dir)
	if len(rows) == 0 {
		b.WriteString(repl.MsgAttachListNone)
		return b.String()
	}
	b.WriteString(repl.MsgAttachHeader)
	for _, r := range rows {
		fmt.Fprintf(&b, repl.MsgAttachRowFmt, r.Entry.PID, padCol(r.Entry.TTY, 7), padCol(attachStateLabel(r), 12), term.Truncate(attachTitle(r.Entry), 60))
	}
	return b.String()
}

func attachStateLabel(r attach.Row) string {
	if !r.Alive {
		return repl.MsgAttachListStale
	}
	switch r.Entry.State {
	case attach.StateBusy:
		if !r.Entry.TurnStarted.IsZero() {
			return "执行中 " + shortDur(time.Since(r.Entry.TurnStarted))
		}
		return "执行中"
	case attach.StateReadOnly:
		return "归档只读"
	}
	return "空闲"
}

func attachTitle(e attach.Entry) string {
	if e.Title == "" {
		return e.Session
	}
	if e.Session == "" {
		return e.Title
	}
	return e.Title + " · " + e.Session
}

func printAttachPrune(st printer, rep attach.PruneReport) {
	st.Print(attachPruneText(rep))
}

// attachPruneText 渲染清理报告（CLI --prune 与 agent_custom prune attach 共用）。
func attachPruneText(rep attach.PruneReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, repl.MsgAttachPruneHead, rep.Dir)
	if len(rep.Removed) == 0 && len(rep.Kept) == 0 {
		b.WriteString(repl.MsgAttachPruneNone)
		return b.String()
	}
	if len(rep.Removed) == 0 {
		b.WriteString(repl.MsgAttachPruneNone)
	}
	for _, it := range rep.Removed {
		fmt.Fprintf(&b, repl.MsgAttachPruneRowFmt, it.PID, padCol(attachKindLabel(it.Kind), 12), attachItemDesc(it))
	}
	for _, it := range rep.Kept {
		fmt.Fprintf(&b, repl.MsgAttachPruneKeptFmt, it.PID, padCol(attachKeptLabel(it.Kind), 12), attachItemDesc(it))
	}
	fmt.Fprintf(&b, repl.MsgAttachPruneTailFmt, len(rep.Removed), len(rep.Kept))
	return b.String()
}

func attachKindLabel(kind string) string {
	switch kind {
	case attach.KindEntry:
		return "陈旧注册"
	case attach.KindSock:
		return "孤儿 socket"
	case attach.KindCorrupt:
		return "损坏注册项"
	case attach.KindTmp:
		return "临时残渣"
	}
	return kind
}

func attachKeptLabel(kind string) string {
	switch kind {
	case attach.KindEntry:
		return "运行实例"
	case attach.KindSock:
		return "socket 有响应"
	case attach.KindCorrupt:
		return "损坏项有响应"
	}
	return kind
}

func attachItemDesc(it attach.PrunedItem) string {
	parts := make([]string, 0, 2)
	if s := attachTitle(attach.Entry{Title: it.Title, Session: it.Session}); s != "" {
		parts = append(parts, s)
	}
	if it.Sock != "" {
		parts = append(parts, it.Sock)
	}
	return strings.Join(parts, "  ")
}

func padCol(s string, w int) string {
	if d := w - term.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func shortDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
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

func applyModel(cfg *config.Config, v string) {
	if m := strings.TrimSpace(v); m != "" {
		cfg.Model = m
	}
}

// systemBase 组装注入 agent 的系统提示基座：内置提示词 + 环境段（OS/shell 执行契约）。
func systemBase(inv shell.Invocation) string {
	return systemPromptFile + "\n\n" + envSection(inv)
}

// envSection 是 agent 初始化头部的环境段，由 main 组装并随基座注入。
func envSection(inv shell.Invocation) string {
	var b strings.Builder
	b.WriteString("# 环境\n")
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "SHELL: %s\n", inv.Name)
	if ctty.Supported {
		b.WriteString("TTY: 交互提示须写入 /dev/tty 才可见（stdout/stderr 被工具捕获）\n")
	}
	fmt.Fprintf(&b, "TIMEOUT: 默认 %ds（interactive 时 %ds），上限 %ds\n", shell.TimeoutSec, shell.InteractiveTimeoutSec, shell.TimeoutLimit)
	fmt.Fprintf(&b, "OUTPUT: stdout/stderr 头尾各 %dKB，中间截断\n", shell.MaxOutput/1000)
	return b.String()
}

// writeUsage 打印用法：模式段为固定文案，选项段由 flag 包按定义清单生成。
func writeUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprint(w, repl.UsageHead)
	old := fs.Output()
	fs.SetOutput(w)
	defer fs.SetOutput(old)
	fs.PrintDefaults()
}

// buildNotifier 按配置组装通知行为（bell / OSC 9 / 外部程序，各自独立开关），全关时为 nil。
func buildNotifier(ui config.UI, inv shell.Invocation) (repl.Notifier, error) {
	var list []repl.Notifier
	if ui.Bell {
		list = append(list, repl.BellNotifier())
	}
	if ui.NotifyOSC {
		list = append(list, repl.OSCNotifier())
	}
	if ui.NotifyCmd != "" {
		n, err := shell.NewCommandNotifier(inv, ui.NotifyCmd)
		if err != nil {
			return nil, err
		}
		list = append(list, n)
	}
	return repl.Notifiers(list...), nil
}

func rootRefusal() error {
	if os.Getenv(envAllowRoot) == "1" || !ctty.IsRoot() {
		return nil
	}
	return errors.New(repl.MsgRootRefused)
}

func exitNow(code int) {
	ctty.RestoreUTF8()
	os.Exit(code)
}

type consoleForShell struct{ con readline.Console }

func (c consoleForShell) LendStdin() (shell.Lease, error) {
	l, err := c.con.LendStdin()
	if l == nil {
		return nil, err
	}
	return l.(shell.Lease), err
}

func (c consoleForShell) LendFull(cmd *exec.Cmd, capture io.Writer) (shell.Lease, error) {
	l, err := c.con.LendFull(cmd, capture)
	if l == nil {
		return nil, err
	}
	return l.(shell.Lease), err
}
