package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/repl"
	"github.com/LaoQi/tanyan/style"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("v", false, repl.FlagVersion)
	configPath := flag.String("c", "", repl.FlagConfig)
	sessionMode := flag.String("m", "", repl.FlagMode)
	flag.Parse()

	if *showVersion {
		fmt.Printf("tanyan %s\n", version)
		return
	}

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, repl.MsgErrLineFmt+"\n", err)
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
	agent.ProtectTerminalSignals()
	a, err := agent.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	sink := repl.WireToolView(repl.ToolWidth, a.ToolOutputLines())

	args := flag.Args()
	if len(args) > 0 && args[0] == "ask" {
		q := strings.Join(args[1:], " ")
		if q == "" {
			fmt.Fprint(os.Stderr, repl.MsgAskUsage)
			os.Exit(1)
		}
		ctx, done := repl.InterruptContext()
		err := a.Ask(ctx, q, sink)
		done()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n"+repl.MsgErrLineFmt+"\n", err)
			os.Exit(1)
		}
		fmt.Println()
		return
	}

	r, err := repl.NewREPL(a, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
	defer r.Close()
	if err := r.Run(); err != nil {
		fmt.Fprintf(os.Stderr, repl.MsgErrLineFmt+"\n", err)
		os.Exit(1)
	}
}
