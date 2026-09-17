package main

import (
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

	cmd, rest := repl.ParseCommand(flag.Args())
	mode, err := repl.ParseMode(*plain, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	if cmd == repl.CmdAsk {
		mode = repl.SingleShot(mode)
	}
	st := repl.NewStreams(os.Stdout, os.Stderr, mode)

	if *showVersion {
		st.Print(fmt.Sprintf("tanya %s\n", version))
		return
	}
	if cmd == repl.CmdAsk && rest == "" {
		st.Fail(repl.MsgAskUsage)
		os.Exit(1)
	}
	if cmd == repl.CmdInit && rest != "" {
		st.Fail(repl.MsgInitUsage)
		os.Exit(1)
	}

	repl.Version = version
	repl.BuildTime = buildTime

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		st.Fail(repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	if err := repl.ValidateTheme(cfg.Theme); err != nil {
		st.Fail(repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
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
			st.Fail(repl.MsgErrLineFmt+"\n", err)
			os.Exit(1)
		}
	}
	agent.ProtectTerminalSignals()
	ctty.WatchSignals()
	readline.InitTerminalGuard()
	readline.SecureTerminal()
	a, err := agent.New(cfg,
		agent.NoSave(*noSave),
		agent.WithTTYBridge(readline.NewTTYBridge()))
	if err != nil {
		st.Fail(repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	termFacts := repl.TermFacts{Cols: facts.Cols, ColsOK: facts.SizeOK}
	sink := repl.NewToolView(st, prof, sem, termFacts.Width, a.ToolOutputLines())

	if cmd == repl.CmdAsk {
		ctx, done := repl.InterruptContext()
		err := a.Ask(ctx, rest, sink.Handle)
		done()
		if err != nil {
			if ctty.Exiting() {
				os.Exit(ctty.ExitStatus())
			}
			st.Fail("\n"+repl.MsgErrLineFmt+"\n", err)
			os.Exit(1)
		}
		st.End()
		if code := ctty.ExitStatus(); code != 0 {
			os.Exit(code)
		}
		return
	}

	r, err := repl.NewREPL(a, "", repl.WithStreams(st), repl.WithTermFacts(termFacts), repl.WithTheme(cfg.Theme, cfg.Palette), repl.WithShowReasoning(cfg.ShowReasoning))
	if err != nil {
		st.Fail(repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	defer r.Close()
	if err := r.Run(); err != nil {
		st.Fail(repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	if code := ctty.ExitStatus(); code != 0 {
		os.Exit(code)
	}
}
