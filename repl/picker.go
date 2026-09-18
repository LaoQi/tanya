package repl

import (
	"fmt"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"
	"io"
	"os"
	"strings"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
)

func sessSummary(s agent.SessionInfo) string {
	if s.Archived {
		return SessArchMark + s.Summary
	}
	return s.Summary
}

type sessionPicker struct {
	items  []agent.SessionInfo
	cursor int
	done   bool
	cancel bool
	sem    theme.Semantics
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
		fmt.Fprint(out, term.CursorUp(len(p.items)+1))
	}
	fmt.Fprint(out, term.ClearLineHome()+PickTitle)
	for i, s := range p.items {
		mark := MsgMarkPlain
		if i == p.cursor {
			mark = p.sem.Ok.Sprint("> ")
		}
		fmt.Fprintf(out, term.ClearLineHome()+SessRow+term.ClearLine()+"\r\n",
			mark, s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, sessSummary(s))
	}
}

func pickSession(dev readline.Terminal, list []agent.SessionInfo, out io.Writer, sem theme.Semantics) (int, bool) {
	if len(list) == 0 {
		return -1, false
	}
	if err := dev.Raw(); err != nil {
		return -1, false
	}
	defer dev.Restore()
	p := &sessionPicker{items: list, sem: sem}
	p.render(out, true)
	for {
		ev, err := dev.ReadKey()
		if err != nil {
			return -1, false
		}
		p.handle(ev)
		if p.done {
			break
		}
		p.render(out, false)
	}
	if p.cancel {
		return -1, false
	}
	return p.cursor, true
}

func pickByNumber(list []agent.SessionInfo, out *output) (int, bool) {
	var b strings.Builder
	b.WriteString(PickNumTitle)
	for i, s := range list {
		fmt.Fprintf(&b, "  %-3d "+SessRow+"\n", i+1, "", s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, sessSummary(s))
	}
	b.WriteString(PickNumPrompt)
	out.emit(KindNotice, b.String())
	var n int
	if _, err := fmt.Fscan(os.Stdin, &n); err != nil || n < 1 || n > len(list) {
		return -1, false
	}
	return n - 1, true
}
