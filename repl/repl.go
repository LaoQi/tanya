package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
	"github.com/LaoQi/tanyan/style"
)

type REPL struct {
	agent     *agent.Agent
	ed        *readline.Editor
	term      readline.Terminal
	raw       bool
	promptTpl string
	prompt    style.Template
	onDelta   func(string)
	md        *style.MarkdownBuf
	mdLive    bool
	rend      style.Renderer
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
	tpl, err := style.ParseTemplate(promptTpl)
	if err != nil {
		return nil, err
	}
	r := &REPL{agent: a, ed: ed, term: term, raw: raw, promptTpl: promptTpl, prompt: tpl, onDelta: func(s string) { fmt.Print(s) }}
	r.md = style.NewMarkdownBuf()
	r.mdLive = true
	r.rend = style.NewRenderer(style.GetProfile())
	if a != nil {
		r.onDelta = WireToolView(a, func() int { return toolWidth(term) }, a.ToolOutputLines())
		innerStart := a.OnToolStart
		a.OnToolStart = func(name, args string) {
			r.flushMd()
			innerStart(name, args)
		}
		innerResp := a.OnResponse
		a.OnResponse = func(info agent.ResponseInfo) {
			r.flushMd()
			innerResp(info)
		}
	}
	return r, nil
}

func (r *REPL) deltaFn() func(string) {
	if !r.mdLive || !style.GetProfile().TTY {
		return r.onDelta
	}
	return func(s string) {
		for _, blk := range r.md.Write(s) {
			r.onDelta(r.rend.Block(blk))
		}
	}
}

func (r *REPL) flushMd() {
	for _, blk := range r.md.Flush() {
		r.onDelta(r.rend.Block(blk))
	}
}

func (r *REPL) resolveVars() func(string) (string, bool) {
	return func(name string) (string, bool) {
		switch name {
		case "cwd":
			return shortCwd(), true
		case "model":
			return r.agent.Model(), true
		case "effort":
			return r.agent.ReasoningEffort(), true
		case "usage":
			return r.agent.PromptUsage(), true
		case "cache":
			return r.agent.PromptCache(), true
		case "cache_rate":
			return r.agent.PromptCacheRate(), true
		case "stat":
			return r.agent.PromptSummary(), true
		}
		return "", false
	}
}

func (r *REPL) Close() {}

func (r *REPL) Run() error {
	fmt.Print(welcomText)
	for {
		prompt := r.prompt.Render(r.resolveVars())
		line, err := r.ed.Readline(prompt)
		if err == readline.ErrInterrupt {
			continue
		}
		if err == io.EOF {
			fmt.Print(MsgBye + "\n")
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
		ctx, done := r.interruptContext()
		r.md.Reset()
		err = r.agent.Ask(ctx, line, r.deltaFn())
		for _, blk := range r.md.Close() {
			r.onDelta(r.rend.Block(blk))
		}
		done()
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, MsgErrLineFmt+"\n", err)
		}
	}
}

func (r *REPL) interruptContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	raw := false
	kw, watchable := r.term.(readline.KeyWatcher)
	if r.raw && watchable {
		if err := kw.WatchRaw(); err == nil {
			raw = true
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					ev, err := kw.ReadKeyUntil(stop)
					if err != nil {
						return
					}
					if ev.Code == readline.KeyCtrlC {
						cancel()
					}
				}
			}()
		}
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-sigCh:
			cancel()
		case <-stop:
		}
	}()
	return ctx, func() {
		cancel()
		close(stop)
		wg.Wait()
		signal.Stop(sigCh)
		if raw {
			r.term.Restore()
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
		fmt.Printf("%s\n", MsgBye)
		return true
	case "/help":
		fmt.Print(helpText)
	case "/new":
		r.agent.NewSession()
		fmt.Print(MsgNewSession)
	case "/sessions":
		list, err := r.agent.ListSessions()
		if err != nil {
			fmt.Printf(MsgErrLineFmt+"\n", err)
			break
		}
		if len(list) == 0 {
			fmt.Print(MsgNoSessions)
			break
		}
		for _, s := range list {
			fmt.Printf(SessRow+"\n", "", s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, s.Summary)
		}
	case "/load":
		if len(parts) >= 2 {
			if err := r.agent.LoadSession(parts[1]); err != nil {
				fmt.Printf(MsgErrLineFmt+"\n", err)
			} else {
				fmt.Printf(MsgLoadedSess, parts[1])
				r.warnLegacyPrompt()
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
			fmt.Printf(MsgCurModel, r.agent.Model())
			models, err := r.agent.ListModels()
			if err != nil {
				fmt.Printf(MsgModelsFail, err)
				break
			}
			if len(models) == 0 {
				fmt.Print(MsgModelsEmpty)
				break
			}
			fmt.Print(MsgModelsHead)
			for _, m := range models {
				mark := MsgMarkPlain
				if m == r.agent.Model() {
					mark = MsgMarkCurrent
				}
				fmt.Printf("%s%s\n", mark, m)
			}
			break
		}
		r.agent.SetModel(parts[1])
	case "/think":
		r.handleThink(parts[1:])
	case "/md":
		r.mdLive = !r.mdLive
		if r.mdLive {
			fmt.Print(MsgMdOn)
		} else {
			fmt.Print(MsgMdOff)
		}
	default:
		fmt.Print(MsgUnknownCmd)
	}
	return false
}

func (r *REPL) handleThink(args []string) {
	if len(args) == 0 {
		if cur := r.agent.ReasoningEffort(); cur == "" {
			fmt.Print(MsgThinkUnset)
		} else {
			fmt.Printf(MsgCurEffort, cur)
		}
		return
	}
	if err := r.agent.SetReasoningEffort(args[0]); err != nil {
		fmt.Printf(MsgErrLineFmt+"\n", err)
		return
	}
	if cur := r.agent.ReasoningEffort(); cur == "" {
		fmt.Print(MsgEffortOff)
	} else {
		fmt.Printf(MsgEffortSet, cur)
	}
}

func (r *REPL) showHistory(args []string) {
	msgs := r.agent.History()
	if len(msgs) == 0 {
		fmt.Print(MsgNoHistoryMsg)
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
			fmt.Printf(MsgInvalidIndex, len(msgs))
			return
		}
		printHistoryFull(n, msgs[n-1])
		return
	}
	fmt.Printf(MsgTotalMsgs, len(msgs))
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
		return fmt.Sprintf(MsgCallLabel, strings.Join(names, ", "))
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
		fmt.Printf(MsgErrLineFmt+"\n", err)
		return
	}
	if len(list) == 0 {
		fmt.Print(MsgNoSessions)
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
		fmt.Print(MsgCancelled)
		return
	}
	if err := r.agent.LoadSession(list[idx].ID); err != nil {
		fmt.Printf(MsgErrLineFmt+"\n", err)
		return
	}
	fmt.Printf(MsgLoadedSess, list[idx].ID)
	r.warnLegacyPrompt()
}

func (r *REPL) warnLegacyPrompt() {
	if r.agent.LegacyPrompt() {
		fmt.Print(MsgLegacyHint)
	}
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
