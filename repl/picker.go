package repl

import (
	"fmt"
	"io"
	"os"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
	"github.com/LaoQi/tanyan/style"
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
		fmt.Fprint(out, style.CursorUp(len(p.items)+1))
	}
	fmt.Fprint(out, style.ClearLineHome()+PickTitle)
	for i, s := range p.items {
		mark := MsgMarkPlain
		if i == p.cursor {
			mark = style.Ok.Sprint("> ")
		}
		fmt.Fprintf(out, style.ClearLineHome()+SessRow+style.ClearLine()+"\r\n",
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
	fmt.Printf(PickNumTitle)
	for i, s := range list {
		fmt.Printf("  %-3d "+SessRow+"\n", i+1, "", s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, s.Summary)
	}
	fmt.Print(PickNumPrompt)
	var n int
	if _, err := fmt.Fscan(os.Stdin, &n); err != nil || n < 1 || n > len(list) {
		return -1, false
	}
	return n - 1, true
}
