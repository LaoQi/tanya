package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
)

const helpText = `斜杠命令：
  /help            显示帮助
  /new             开启新会话（当前会话自动保存）
  /sessions        列出历史会话
  /load <id>       载入历史会话
  /context         显示上下文占用
  /history [n|all] 无参截断列表；n 全量查看单条；all 全量显示
  /model [name]    无参显示当前模型；带名切换模型
  /exit            退出
直接输入文本与 AI 对话；shell 工具直接执行，无需确认。
`

const welcomText = `
██████ ▄████▄ ███  ██ ██  ██ ▄████▄ 
  ██   ██▄▄██ ██ ▀▄██  ▀██▀  ██▄▄██ 
  ██   ██  ██ ██   ██   ██   ██  ██ 

输入 /help 查看命令

`

type REPL struct {
	agent     *agent.Agent
	ed        *readline.Editor
	term      readline.Terminal
	raw       bool
	promptTpl string
}

func NewREPL(a *agent.Agent, promptTpl string) (*REPL, error) {
	term, raw := readline.NewTerminal()
	ed := readline.NewEditor(term, raw)
	c := &completer{listSessions: a.ListSessions, listModels: a.ListModels}
	ed.SetComplete(c.complete)
	ed.SetGhost(c.suggest)
	if promptTpl == "" {
		promptTpl = agent.DefaultPrompt
	}
	return &REPL{agent: a, ed: ed, term: term, raw: raw, promptTpl: promptTpl}, nil
}

func renderPrompt(tpl, cwd, model, usage, cache, cacheRate string) string {
	return strings.NewReplacer(
		"{cwd}", cwd,
		"{model}", model,
		"{usage}", usage,
		"{cache}", cache,
		"{cache_rate}", cacheRate,
	).Replace(tpl)
}

func (r *REPL) Close() {}

func (r *REPL) Run() error {
	fmt.Print(welcomText)
	for {
		prompt := renderPrompt(r.promptTpl, shortCwd(), r.agent.Model(), r.agent.PromptUsage(), r.agent.PromptCache(), r.agent.PromptCacheRate())
		line, err := r.ed.Readline(prompt)
		if err == readline.ErrInterrupt {
			continue
		}
		if err == io.EOF {
			fmt.Println("Bye")
			return nil
		}
		if err != nil {
			return err
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
		ctx, done := InterruptContext()
		err = r.agent.Ask(ctx, line, func(s string) { fmt.Print(s) })
		done()
		fmt.Println()
		if err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
		}
	}
}

func InterruptContext() (context.Context, func()) {
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
		if len(parts) >= 2 {
			if err := r.agent.LoadSession(parts[1]); err != nil {
				fmt.Println("错误:", err)
			} else {
				fmt.Println("已载入会话", parts[1])
			}
			break
		}
		r.loadSessionInteractive()
	case "/context":
		fmt.Println(r.agent.ContextInfo())
	case "/history":
		r.showHistory(parts[1:])
	case "/model":
		if len(parts) < 2 {
			fmt.Println("当前模型:", r.agent.Model())
			models, err := r.agent.ListModels()
			if err != nil {
				fmt.Println("获取可用模型失败:", err)
				break
			}
			if len(models) == 0 {
				fmt.Println("(接口未返回可用模型)")
				break
			}
			fmt.Println("可用模型:")
			for _, m := range models {
				mark := "  "
				if m == r.agent.Model() {
					mark = "* "
				}
				fmt.Println(mark + m)
			}
			break
		}
		r.agent.SetModel(parts[1])
	default:
		fmt.Println("未知命令，输入 /help 查看")
	}
	return false
}

func (r *REPL) showHistory(args []string) {
	msgs := r.agent.History()
	if len(msgs) == 0 {
		fmt.Println("(当前会话无消息)")
		return
	}
	if len(args) > 0 {
		if args[0] == "all" {
			for i, m := range msgs {
				if i > 0 {
					fmt.Println()
				}
				printHistoryFull(i+1, m)
			}
			return
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 || n > len(msgs) {
			fmt.Printf("序号无效（1-%d，或 all）\n", len(msgs))
			return
		}
		printHistoryFull(n, msgs[n-1])
		return
	}
	fmt.Printf("共 %d 条消息\n", len(msgs))
	for i, m := range msgs {
		fmt.Println(historyLine(i+1, m))
	}
}

func historyLabel(m agent.Message) string {
	if m.Role == "tool" && m.Name != "" {
		return m.Name
	}
	return m.Role
}

func historyText(m agent.Message) string {
	if m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) > 0 {
		names := make([]string, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			names[i] = tc.Function.Name
		}
		return "[调用 " + strings.Join(names, ", ") + "]"
	}
	return m.Content
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func historyLine(n int, m agent.Message) string {
	text := strings.ReplaceAll(historyText(m), "\n", " ")
	return fmt.Sprintf("%3d %-9s %s", n, historyLabel(m), truncateRunes(text, 120))
}

func printHistoryFull(n int, m agent.Message) {
	fmt.Printf("#%d %s\n", n, historyLabel(m))
	if text := historyText(m); text != "" {
		fmt.Println(text)
	}
	for _, tc := range m.ToolCalls {
		fmt.Printf("→ %s %s\n", tc.Function.Name, tc.Function.Arguments)
	}
}

func (r *REPL) loadSessionInteractive() {
	list, err := r.agent.ListSessions()
	if err != nil {
		fmt.Println("错误:", err)
		return
	}
	if len(list) == 0 {
		fmt.Println("(无历史会话)")
		return
	}
	var idx int
	var ok bool
	if r.raw {
		idx, ok = pickSession(r.term, list)
	} else {
		idx, ok = pickByNumber(list)
	}
	if !ok || idx < 0 {
		fmt.Println("已取消")
		return
	}
	if err := r.agent.LoadSession(list[idx].ID); err != nil {
		fmt.Println("错误:", err)
		return
	}
	fmt.Println("已载入会话", list[idx].ID)
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
