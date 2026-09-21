package main

import (
	_ "embed"
	"flag"
	"fmt"
	"github.com/LaoQi/tanya/render/term"
	"os"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/ctty"
	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/repl"
)

var (
	version   = "dev"
	buildTime = ""
)

//go:embed system_prompt.md
var systemPromptFile string

//go:embed config.example.yaml
var configExampleFile string

func main() {
	showVersion := flag.Bool("v", false, repl.FlagVersion)
	configPath := flag.String("c", "", repl.FlagConfig)
	sessionMode := flag.String("m", "", repl.FlagMode)
	noSave := flag.Bool("n", false, repl.FlagNoSave)
	flag.BoolVar(noSave, "no-save", false, repl.FlagNoSave)
	plain := flag.Bool("p", false, repl.FlagPlain)
	flag.BoolVar(plain, "plain", false, repl.FlagPlain)
	verbose := flag.Bool("verbose", false, repl.FlagVerbose)
	flag.Parse()
	ctty.EnsureUTF8()
	defer ctty.RestoreUTF8()

	cmd, rest := repl.ParseCommand(flag.Args())
	mode, err := repl.ParseMode(*plain, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, repl.MsgErrLineFmt+"\n", err)
		exitNow(1)
	}
	if cmd == repl.CmdAsk {
		mode = repl.SingleShot(mode)
	}
	st := repl.NewStreams(os.Stdout, os.Stderr, mode)

	if *showVersion {
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

	cfg, err := agent.LoadConfig(*configPath)
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
	if *plain {
		prof.Colors = term.LevelNone
	}
	term.SetProfile(prof)
	if *sessionMode != "" {
		cfg.SessionMode = *sessionMode
	}
	if cmd == repl.CmdInit {
		if err := repl.RunInit(st, sem, cfg); err != nil {
			st.FailErr("", err)
			exitNow(1)
		}
	}
	agent.ProtectTerminalSignals()
	ctty.WatchSignals()
	readline.InitTerminalGuard()
	readline.SecureTerminal()
	a, err := agent.New(cfg,
		agent.NoSave(*noSave),
		agent.WithTTYBridge(readline.NewTTYBridge()),
		agent.WithSystemPrompt(systemPromptFile))
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	termFacts := repl.TermFacts{Cols: facts.Cols, ColsOK: facts.SizeOK}
	sink := repl.NewToolView(st, prof, sem, termFacts.Width, a.ToolOutputLines())

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

	notifier, err := buildNotifier(cfg)
	if err != nil {
		st.FailErr("", err)
		exitNow(1)
	}
	r, err := repl.NewREPL(a, "", repl.WithStreams(st), repl.WithTermFacts(termFacts), repl.WithTheme(cfg.Theme, cfg.Palette), repl.WithShowReasoning(cfg.ShowReasoning), repl.WithNotifier(notifier))
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

// buildNotifier 按配置组装通知行为（bell / OSC 9 / 外部程序，各自独立开关），全关时为 nil。
func buildNotifier(cfg *agent.Config) (repl.Notifier, error) {
	var list []repl.Notifier
	if cfg.Bell {
		list = append(list, repl.BellNotifier())
	}
	if cfg.NotifyOSC {
		list = append(list, repl.OSCNotifier())
	}
	if cfg.NotifyCmd != "" {
		inv, err := agent.ResolveShell(cfg)
		if err != nil {
			return nil, err
		}
		n, err := repl.NewCommandNotifier(inv, cfg.NotifyCmd)
		if err != nil {
			return nil, err
		}
		list = append(list, n)
	}
	return repl.Notifiers(list...), nil
}

func exitNow(code int) {
	ctty.RestoreUTF8()
	os.Exit(code)
}
