package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/LaoQi/tanya/render/term"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/config"
	"github.com/LaoQi/tanya/ctty"
	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/repl"
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
	term.SetProfile(prof)
	if *f.sessionMode != "" {
		cfg.SessionMode = *f.sessionMode
	}
	if cmd == repl.CmdInit {
		if err := repl.RunInit(st, sem, &cfg.Config); err != nil {
			st.FailErr("", err)
			exitNow(1)
		}
	}
	agent.ProtectTerminalSignals()
	ctty.WatchSignals()
	readline.InitTerminalGuard()
	readline.SecureTerminal()
	a, err := agent.New(&cfg.Config,
		agent.NoSave(*f.noSave),
		agent.WithTTYBridge(readline.NewTTYBridge()),
		agent.WithSystemPrompt(systemPromptFile))
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	termFacts := repl.TermFacts{Cols: facts.Cols, ColsOK: facts.SizeOK}
	sink := repl.NewToolView(st, prof, sem, termFacts.Width, cfg.ToolOutputLines)

	if cmd == repl.CmdAsk {
		ctx, done := repl.InterruptContext()
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

	notifier, err := buildNotifier(cfg.UI, &cfg.Config)
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	r, err := repl.NewREPL(a, "", repl.WithStreams(st), repl.WithTermFacts(termFacts), repl.WithTheme(cfg.Theme, cfg.Palette), repl.WithShowReasoning(cfg.ShowReasoning), repl.WithNotifier(notifier), repl.WithToolOutputLines(cfg.ToolOutputLines))
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

// writeUsage 打印用法：模式段为固定文案，选项段由 flag 包按定义清单生成。
func writeUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprint(w, repl.UsageHead)
	old := fs.Output()
	fs.SetOutput(w)
	defer fs.SetOutput(old)
	fs.PrintDefaults()
}

// buildNotifier 按配置组装通知行为（bell / OSC 9 / 外部程序，各自独立开关），全关时为 nil。
func buildNotifier(ui config.UI, acfg *agent.Config) (repl.Notifier, error) {
	var list []repl.Notifier
	if ui.Bell {
		list = append(list, repl.BellNotifier())
	}
	if ui.NotifyOSC {
		list = append(list, repl.OSCNotifier())
	}
	if ui.NotifyCmd != "" {
		inv, err := agent.ResolveShell(acfg)
		if err != nil {
			return nil, err
		}
		n, err := repl.NewCommandNotifier(inv, ui.NotifyCmd)
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
