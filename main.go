package main

import (
	"flag"
	"fmt"
	"github.com/LaoQi/tanyan/render/term"
	"os"
	"strings"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
	"github.com/LaoQi/tanyan/repl"
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

	args := flag.Args()
	isAsk := len(args) > 0 && args[0] == "ask"
	mode, err := repl.ParseMode(*plain, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	if isAsk {
		mode = repl.SingleShot(mode)
	}
	st := repl.NewStreams(os.Stdout, os.Stderr, mode)

	if *showVersion {
		st.Print(fmt.Sprintf("tanyan %s\n", version))
		return
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
	prof := term.DetectProfile(repl.ToolTTY())
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
	agent.ProtectTerminalSignals()
	readline.InitTerminalGuard()
	readline.SecureTerminal()
	a, err := agent.New(cfg,
		agent.NoSave(*noSave),
		agent.WithTTYBridge(readline.NewTTYBridge()))
	if err != nil {
		st.Fail(repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	sink := repl.NewToolView(st, prof, sem, repl.ToolWidth, a.ToolOutputLines())

	if isAsk {
		q := strings.Join(args[1:], " ")
		if q == "" {
			st.Fail(repl.MsgAskUsage)
			os.Exit(1)
		}
		ctx, done := repl.InterruptContext()
		err := a.Ask(ctx, q, sink.Handle)
		done()
		if err != nil {
			st.Fail("\n"+repl.MsgErrLineFmt+"\n", err)
			os.Exit(1)
		}
		st.End()
		return
	}

	r, err := repl.NewREPL(a, "", repl.WithStreams(st), repl.WithTheme(cfg.Theme, cfg.Palette))
	if err != nil {
		st.Fail(repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	defer r.Close()
	if err := r.Run(); err != nil {
		st.Fail(repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
}
