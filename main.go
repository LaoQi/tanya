package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

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
	a.OnTool = func(name, args, result string) {
		r := strings.ReplaceAll(result, "\n", " ")
		if utf8.RuneCountInString(r) > 120 {
			r = string([]rune(r)[:120]) + "..."
		}
		fmt.Printf("\n\033[90m[tool] %s(%s) => %s\033[0m\n", name, args, r)
	}

	args := flag.Args()
	if len(args) > 0 && args[0] == "ask" {
		q := strings.Join(args[1:], " ")
		if q == "" {
			fmt.Fprintln(os.Stderr, "用法: tanyan ask \"问题\"")
			os.Exit(1)
		}
		ctx, done := repl.InterruptContext()
		err := a.Ask(ctx, q, func(s string) { fmt.Print(s) })
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
