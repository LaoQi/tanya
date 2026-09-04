package readline

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
)

var ErrInterrupt = errors.New("interrupted")

type Completion struct {
	Insert  string
	Display string
}

func (c Completion) display() string {
	if c.Display == "" {
		return c.Insert
	}
	return c.Display
}

type Editor struct {
	term     Terminal
	raw      bool
	history  []string
	draft    string
	histIdx  int
	complete func(line string) []Completion
	ghostFn  func(line string) string
	ghost    string
	out      io.Writer
	buf      []rune
	pos      int
	prompt   string
	kill     string
}

func NewEditor(term Terminal, raw bool) *Editor {
	return &Editor{term: term, raw: raw, out: os.Stdout}
}

func (e *Editor) SetComplete(fn func(string) []Completion) { e.complete = fn }

func (e *Editor) SetGhost(fn func(string) string) { e.ghostFn = fn }

func (e *Editor) SetOutput(w io.Writer) { e.out = w }

func (e *Editor) History() []string { return e.history }

func (e *Editor) Readline(prompt string) (string, error) {
	e.prompt = prompt
	if !e.raw {
		fmt.Fprint(e.out, prompt)
		ev, err := e.term.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", io.EOF
			}
			return "", err
		}
		if ev.Code == KeyLine {
			return ev.Text, nil
		}
		return "", io.EOF
	}
	if err := e.term.Raw(); err != nil {
		fmt.Fprint(e.out, prompt)
		ev, err2 := e.term.ReadKey()
		if err2 != nil {
			return "", err2
		}
		if ev.Code == KeyLine {
			return ev.Text, nil
		}
		return "", io.EOF
	}
	defer e.term.Restore()

	e.buf = nil
	e.pos = 0
	e.histIdx = len(e.history)
	e.draft = ""
	e.render("")
	for {
		ev, err := e.term.ReadKey()
		if err != nil {
			return "", err
		}
		done, line, rerr := e.handleKey(ev)
		if done {
			return line, rerr
		}
	}
}

func (e *Editor) handleKey(ev KeyEvent) (bool, string, error) {
	switch ev.Code {
	case KeyRune:
		e.insert(ev.Rune)
	case KeyEnter:
		line := string(e.buf)
		fmt.Fprint(e.out, "\r\n")
		if strings.TrimSpace(line) != "" {
			e.history = append(e.history, line)
		}
		return true, line, nil
	case KeyBackspace:
		if e.pos > 0 {
			e.buf = append(e.buf[:e.pos-1], e.buf[e.pos:]...)
			e.pos--
		}
	case KeyDelete:
		if e.pos < len(e.buf) {
			e.buf = append(e.buf[:e.pos], e.buf[e.pos+1:]...)
		}
	case KeyLeft, KeyCtrlB:
		if e.pos > 0 {
			e.pos--
		}
	case KeyRight, KeyCtrlF:
		if e.pos < len(e.buf) {
			e.pos++
		} else if e.ghost != "" {
			ghost := []rune(e.ghost)
			e.buf = append(e.buf, ghost...)
			e.pos = len(e.buf)
		}
	case KeyCtrlU:
		e.kill = string(e.buf[:e.pos])
		e.buf = append(e.buf[:0], e.buf[e.pos:]...)
		e.pos = 0
	case KeyCtrlK:
		e.kill = string(e.buf[e.pos:])
		e.buf = e.buf[:e.pos]
	case KeyCtrlW:
		s := e.wordBack(e.pos)
		e.kill = string(e.buf[s:e.pos])
		e.buf = append(e.buf[:s], e.buf[e.pos:]...)
		e.pos = s
	case KeyCtrlY:
		for _, r := range e.kill {
			e.insert(r)
		}
	case KeyCtrlT:
		e.transpose()
	case KeyCtrlL:
		fmt.Fprint(e.out, "\x1b[2J\x1b[H")
	case KeyAltB:
		e.pos = e.wordBack(e.pos)
	case KeyAltF:
		e.pos = e.wordForward(e.pos)
	case KeyHome, KeyCtrlA:
		e.pos = 0
	case KeyEnd, KeyCtrlE:
		e.pos = len(e.buf)
	case KeyUp:
		e.histPrev()
	case KeyDown:
		e.histNext()
	case KeyCtrlC:
		e.buf = nil
		e.pos = 0
		fmt.Fprint(e.out, "\r\n")
		return true, "", ErrInterrupt
	case KeyCtrlD:
		if len(e.buf) == 0 {
			fmt.Fprint(e.out, "\r\n")
			return true, "", io.EOF
		}
		if e.pos < len(e.buf) {
			e.buf = append(e.buf[:e.pos], e.buf[e.pos+1:]...)
		}
	case KeyTab, KeyEsc:
		if ev.Code == KeyTab && e.complete != nil {
			e.tabComplete()
		}
	}
	e.refreshGhost()
	e.render("")
	return false, "", nil
}

func (e *Editor) refreshGhost() {
	e.ghost = ""
	if e.ghostFn != nil && e.pos == len(e.buf) && len(e.buf) > 0 {
		e.ghost = e.ghostFn(string(e.buf))
	}
}

func (e *Editor) tabComplete() {
	cands := e.complete(string(e.buf))
	if len(cands) == 0 {
		return
	}
	if len(cands) == 1 {
		e.setBuf(cands[0].Insert)
		return
	}
	inserts := make([]string, len(cands))
	for i, c := range cands {
		inserts[i] = c.Insert
	}
	common := commonPrefix(inserts)
	if len([]rune(common)) > len(e.buf) {
		e.setBuf(common)
		return
	}
	fmt.Fprint(e.out, "\r\n")
	for _, c := range cands {
		fmt.Fprintf(e.out, "  %s\r\n", c.display())
	}
}

func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	prefix := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

func (e *Editor) insert(r rune) {
	e.buf = append(e.buf, 0)
	copy(e.buf[e.pos+1:], e.buf[e.pos:])
	e.buf[e.pos] = r
	e.pos++
}

func (e *Editor) setBuf(s string) {
	e.buf = []rune(s)
	e.pos = len(e.buf)
}

func (e *Editor) wordBack(pos int) int {
	for pos > 0 && unicode.IsSpace(e.buf[pos-1]) {
		pos--
	}
	for pos > 0 && !unicode.IsSpace(e.buf[pos-1]) {
		pos--
	}
	return pos
}

func (e *Editor) wordForward(pos int) int {
	n := len(e.buf)
	for pos < n && unicode.IsSpace(e.buf[pos]) {
		pos++
	}
	for pos < n && !unicode.IsSpace(e.buf[pos]) {
		pos++
	}
	return pos
}

func (e *Editor) transpose() {
	if e.pos < len(e.buf) {
		if e.pos == 0 {
			return
		}
		e.buf[e.pos-1], e.buf[e.pos] = e.buf[e.pos], e.buf[e.pos-1]
		e.pos++
	} else if e.pos >= 2 {
		e.buf[e.pos-2], e.buf[e.pos-1] = e.buf[e.pos-1], e.buf[e.pos-2]
	}
}

func (e *Editor) histPrev() {
	if e.histIdx == len(e.history) {
		e.draft = string(e.buf)
	}
	if e.histIdx > 0 {
		e.histIdx--
		e.setBuf(e.history[e.histIdx])
	}
}

func (e *Editor) histNext() {
	if e.histIdx >= len(e.history) {
		return
	}
	e.histIdx++
	if e.histIdx == len(e.history) {
		e.setBuf(e.draft)
	} else {
		e.setBuf(e.history[e.histIdx])
	}
}

func (e *Editor) render(extra string) {
	cur := stringWidth(stripANSI(e.prompt)) + stringWidth(string(e.buf[:e.pos]))
	line := e.prompt + string(e.buf)
	if e.ghost != "" && e.pos == len(e.buf) {
		line += "\x1b[90m" + e.ghost + "\x1b[0m"
	}
	fmt.Fprint(e.out, "\r\x1b[K"+line)
	if cur > 0 {
		fmt.Fprint(e.out, "\r\x1b["+strconv.Itoa(cur)+"C")
	}
}
