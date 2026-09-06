package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/repl"
)

func main() {
	configPath := flag.String("c", "", "配置文件路径（默认 ~/.config/tanyan/config.yaml）")
	sessionMode := flag.String("m", "", "会话存储模式 local/global/auto（默认 auto）")
	flag.Parse()

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
	if *sessionMode != "" {
		cfg.SessionMode = *sessionMode
	}
	a, err := agent.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
	onDelta := repl.WireToolView(a, func() int { return repl.ToolWidth() }, a.ToolOutputLines(), repl.ToolTTY())

	args := flag.Args()
	if len(args) > 0 && args[0] == "ask" {
		q := strings.Join(args[1:], " ")
		if q == "" {
			fmt.Fprintln(os.Stderr, "用法: tanyan ask \"问题\"")
			os.Exit(1)
		}
		ctx, done := repl.InterruptContext()
		err := a.Ask(ctx, q, onDelta)
		done()
		if err != nil {
			fmt.Fprintln(os.Stderr, "\n错误:", err)
			os.Exit(1)
		}
		fmt.Println()
		return
	}

	r, err := repl.NewREPL(a, cfg.Prompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
	defer r.Close()
	if err := r.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
