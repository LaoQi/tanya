package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"unicode/utf8"

	"github.com/LaoQi/tanyan/agent"
	"github.com/chzyer/readline"
)

const helpText = `斜杠命令：
  /help            显示帮助
  /new             开启新会话（当前会话自动保存）
  /sessions        列出历史会话
  /load <id>       载入历史会话
  /context         显示上下文占用
  /model [name]    无参显示当前模型；带名切换模型
  /exit            退出
直接输入文本与 AI 对话；shell 工具直接执行，无需确认。
`

type REPL struct {
	agent *agent.Agent
	rl    *readline.Instance
}

func NewREPL(a *agent.Agent) (*REPL, error) {
	rl, err := readline.New("")
	if err != nil {
		return nil, err
	}
	return &REPL{agent: a, rl: rl}, nil
}

func (r *REPL) Close() { r.rl.Close() }

func (r *REPL) Run() error {
	fmt.Println("tanyan - 极简 CLI Agent，输入 /help 查看命令")
	for {
		r.rl.SetPrompt(fmt.Sprintf("\033[32m%s(%s|%s)>\033[0m ", shortCwd(), r.agent.Model(), r.agent.PromptUsage()))
		line, err := r.rl.Readline()
		if err == readline.ErrInterrupt {
			if len(line) == 0 {
				fmt.Println("再见")
				return nil
			}
			continue
		} else if err == io.EOF {
			fmt.Println("再见")
			return nil
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if r.handleCommand(line) {
				return nil
			}
			continue
		}
		ctx, done := interruptContext()
		err = r.agent.Ask(ctx, line, func(s string) { fmt.Print(s) })
		done()
		fmt.Println()
		if err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
		}
	}
}

func interruptContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	finished := make(chan struct{})
	go func() {
		select {
		case <-ch:
			fmt.Println("\n^C")
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
		close(finished)
	}()
	return ctx, func() {
		cancel()
		<-finished
	}
}

func (r *REPL) handleCommand(line string) bool {
	parts := strings.Fields(line)
	switch parts[0] {
	case "/exit", "/quit":
		fmt.Println("再见")
		return true
	case "/help":
		fmt.Print(helpText)
	case "/new":
		r.agent.NewSession()
		fmt.Println("已开启新会话")
	case "/sessions":
		list, err := r.agent.ListSessions()
		if err != nil {
			fmt.Println("错误:", err)
			break
		}
		if len(list) == 0 {
			fmt.Println("(无历史会话)")
			break
		}
		for _, s := range list {
			fmt.Printf("%s  %s  %3d条  %s\n", s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, s.Summary)
		}
	case "/load":
		if len(parts) < 2 {
			fmt.Println("用法: /load <session-id>")
			break
		}
		if err := r.agent.LoadSession(parts[1]); err != nil {
			fmt.Println("错误:", err)
		} else {
			fmt.Println("已载入会话", parts[1])
		}
	case "/context":
		fmt.Println(r.agent.ContextInfo())
	case "/model":
		if len(parts) < 2 {
			fmt.Println("当前模型:", r.agent.Model())
			break
		}
		r.agent.SetModel(parts[1])
	default:
		fmt.Println("未知命令，输入 /help 查看")
	}
	return false
}

func shortCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if home, _ := os.UserHomeDir(); home != "" && (cwd == home || strings.HasPrefix(cwd, home+"/")) {
		cwd = "~" + cwd[len(home):]
	}
	parts := strings.Split(cwd, "/")
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == "" {
			continue
		}
		if strings.HasPrefix(parts[i], ".") && len(parts[i]) > 1 {
			r, _ := utf8.DecodeRuneInString(parts[i][1:])
			parts[i] = "." + string(r)
		} else {
			r, _ := utf8.DecodeRuneInString(parts[i])
			parts[i] = string(r)
		}
	}
	return strings.Join(parts, "/")
}
