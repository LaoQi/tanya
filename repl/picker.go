package repl

import (
	"fmt"
	"io"
	"os"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
)

type sessionPicker struct {
	items  []agent.SessionInfo
	cursor int
	done   bool
	cancel bool
}

func (p *sessionPicker) handle(ev readline.KeyEvent) {
	switch ev.Code {
	case readline.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
	case readline.KeyDown:
		if p.cursor < len(p.items)-1 {
			p.cursor++
		}
	case readline.KeyEnter:
		p.done = true
	case readline.KeyEsc, readline.KeyCtrlC, readline.KeyCtrlD:
		p.done = true
		p.cancel = true
	case readline.KeyRune:
		if ev.Rune == 'q' {
			p.done = true
			p.cancel = true
		}
	}
}

func (p *sessionPicker) render(out io.Writer, first bool) {
	if !first && len(p.items) > 0 {
		fmt.Fprintf(out, "\x1b[%dA", len(p.items)+1)
	}
	fmt.Fprint(out, "\r\x1b[K选择会话（↑/↓ 移动，Enter 确认，q 取消）:\r\n")
	for i, s := range p.items {
		mark := "  "
		if i == p.cursor {
			mark = "\x1b[32m> \x1b[0m"
		}
		fmt.Fprintf(out, "\r\x1b[K%s%s  %s  %3d条  %s\x1b[K\r\n",
			mark, s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, s.Summary)
	}
}

func pickSession(term readline.Terminal, list []agent.SessionInfo) (int, bool) {
	if len(list) == 0 {
		return -1, false
	}
	if err := term.Raw(); err != nil {
		return -1, false
	}
	defer term.Restore()
	p := &sessionPicker{items: list}
	p.render(os.Stdout, true)
	for {
		ev, err := term.ReadKey()
		if err != nil {
			return -1, false
		}
		p.handle(ev)
		if p.done {
			break
		}
		p.render(os.Stdout, false)
	}
	if p.cancel {
		return -1, false
	}
	return p.cursor, true
}

func pickByNumber(list []agent.SessionInfo) (int, bool) {
	fmt.Printf("输入序号选择会话（回车取消）:\n")
	for i, s := range list {
		fmt.Printf("  %-3d %s  %s  %3d条  %s\n", i+1, s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, s.Summary)
	}
	fmt.Print("序号: ")
	var n int
	if _, err := fmt.Fscan(os.Stdin, &n); err != nil || n < 1 || n > len(list) {
		return -1, false
	}
	return n - 1, true
}
