package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
	"github.com/LaoQi/tanyan/repl"
	"github.com/LaoQi/tanyan/style"
)

var (
	version   = "dev"
	buildTime = ""
)

func main() {
	st := repl.NewStreams(os.Stdout, os.Stderr)
	showVersion := flag.Bool("v", false, repl.FlagVersion)
	configPath := flag.String("c", "", repl.FlagConfig)
	sessionMode := flag.String("m", "", repl.FlagMode)
	noSave := flag.Bool("n", false, repl.FlagNoSave)
	flag.BoolVar(noSave, "no-save", false, repl.FlagNoSave)
	flag.Parse()

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
	style.ApplyScheme(cfg.Theme)
	style.ApplyPalette(cfg.Palette)
	prof := style.DetectProfile(repl.ToolTTY())
	switch cfg.Colors {
	case "on":
		if prof.Colors == style.LevelNone {
			prof.Colors = style.Level16
		}
	case "off":
		prof.Colors = style.LevelNone
	}
	style.SetProfile(prof)
	if *sessionMode != "" {
		cfg.SessionMode = *sessionMode
	}
	args := flag.Args()
	isAsk := len(args) > 0 && args[0] == "ask"
	agent.ProtectTerminalSignals()
	readline.InitTerminalGuard()
	readline.SecureTerminal()
	agent.InitTTYBridge(readline.NewTTYBridge())
	a, err := agent.New(cfg, agent.NoSave(*noSave))
	if err != nil {
		st.Fail(repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	sink := repl.WireToolView(st, prof, repl.ToolWidth, a.ToolOutputLines())

	if isAsk {
		q := strings.Join(args[1:], " ")
		if q == "" {
			st.Fail(repl.MsgAskUsage)
			os.Exit(1)
		}
		ctx, done := repl.InterruptContext()
		err := a.Ask(ctx, q, sink)
		done()
		if err != nil {
			st.Fail("\n"+repl.MsgErrLineFmt+"\n", err)
			os.Exit(1)
		}
		st.Content("\n")
		return
	}

	r, err := repl.NewREPL(a, "", repl.WithStreams(st))
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
