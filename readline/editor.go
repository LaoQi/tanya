package readline

import (
	"errors"
	"fmt"
	rstyle "github.com/LaoQi/tanya/render/style"
	"github.com/LaoQi/tanya/render/term"
	"io"
	"os"
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
	term          Terminal
	raw           bool
	history       []string
	draft         string
	histIdx       int
	complete      func(line string) []Completion
	ghostFn       func(line string) string
	historyFilter func(string) bool
	ghost         string
	out           io.Writer
	buf           []rune
	pos           int
	prompt        string
	kill          string
	cursorRow     int
	rowsUsed      int
	menu          []Completion
	menuIdx       int
	dim           rstyle.Style
	accent        rstyle.Style
}

func NewEditor(term Terminal, raw bool) *Editor {
	return &Editor{term: term, raw: raw, out: os.Stdout}
}

func (e *Editor) SetComplete(fn func(string) []Completion) { e.complete = fn }

func (e *Editor) SetGhost(fn func(string) string) { e.ghostFn = fn }

func (e *Editor) SetHistoryFilter(fn func(string) bool) { e.historyFilter = fn }

func (e *Editor) SetOutput(w io.Writer) { e.out = w }

func (e *Editor) SetStyles(dim, accent rstyle.Style) {
	e.dim, e.accent = dim, accent
}

func (e *Editor) History() []string { return e.history }

func (e *Editor) Readline(prompt string) (string, error) {
	if exitRequested() {
		return "", ErrExited
	}
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
	e.cursorRow = 0
	e.rowsUsed = 1
	e.menu = nil
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
	if len(e.menu) > 0 {
		switch ev.Code {
		case KeyDown, KeyTab:
			e.menuIdx = (e.menuIdx + 1) % len(e.menu)
			e.render("")
			return false, "", nil
		case KeyUp:
			e.menuIdx = (e.menuIdx - 1 + len(e.menu)) % len(e.menu)
			e.render("")
			return false, "", nil
		case KeyEsc:
			e.menu = nil
			e.render("")
			return false, "", nil
		case KeyEnter:
			e.setBuf(e.menu[e.menuIdx].Insert)
			e.menu = nil
			e.refreshGhost()
			e.render("")
			return false, "", nil
		default:
			e.menu = nil
		}
	}
	switch ev.Code {
	case KeyRune:
		e.insert(ev.Rune)
	case KeyEnter:
		line := string(e.buf)
		e.ghost = ""
		e.render("")
		fmt.Fprint(e.out, "\r\n")
		e.cursorRow = 0
		e.rowsUsed = 1
		if strings.TrimSpace(line) != "" && e.keepHistory(line) {
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
		e.clearKeepHistory()
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
		e.ghost = ""
		e.render("")
		fmt.Fprint(e.out, "\r\n")
		e.cursorRow = 0
		e.rowsUsed = 1
		return true, "", ErrInterrupt
	case KeyCtrlD:
		if len(e.buf) == 0 {
			e.ghost = ""
			e.render("")
			fmt.Fprint(e.out, "\r\n")
			e.cursorRow = 0
			e.rowsUsed = 1
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

func (e *Editor) keepHistory(line string) bool {
	if e.historyFilter != nil {
		return e.historyFilter(line)
	}
	return true
}

func (e *Editor) clearKeepHistory() {
	size, ok := e.term.Size()
	if !ok || size.Rows < 1 {
		fmt.Fprint(e.out, term.ScreenHome())
		e.cursorRow = 0
		e.rowsUsed = 1
		return
	}
	var b strings.Builder
	b.WriteString(strings.Repeat("\n", size.Rows-1))
	b.WriteString(term.LineStart())
	if size.Rows > 1 {
		b.WriteString(term.CursorUp(size.Rows - 1))
	}
	fmt.Fprint(e.out, b.String())
	e.cursorRow = 0
	e.rowsUsed = 1
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
	if _, ok := e.term.Size(); !ok {
		return
	}
	e.menu = cands
	e.menuIdx = 0
}

const menuMax = 8

func (e *Editor) menuLines(cols int) []string {
	if len(e.menu) == 0 {
		return nil
	}
	start := 0
	if len(e.menu) > menuMax {
		start = e.menuIdx - menuMax/2
		if start < 0 {
			start = 0
		}
		if max := len(e.menu) - menuMax; start > max {
			start = max
		}
	}
	end := start + menuMax
	if end > len(e.menu) {
		end = len(e.menu)
	}
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		item := "  " + e.menu[i].display()
		if i == e.menuIdx {
			item = "  " + e.accent.Sprint(e.menu[i].display())
		}
		out = append(out, truncate(item, cols))
	}
	return out
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
	line := e.prompt + string(e.buf)
	if e.ghost != "" && e.pos == len(e.buf) {
		line += e.dim.Sprint(e.ghost)
	}
	cur := stringWidth(stripANSI(e.prompt)) + stringWidth(string(e.buf[:e.pos]))
	size, ok := e.term.Size()
	if !ok || size.Cols <= 0 {
		fmt.Fprint(e.out, term.ClearLineHome()+line)
		if cur > 0 {
			fmt.Fprint(e.out, term.CursorForward(cur))
		}
		e.rowsUsed = 1
		return
	}
	cols := size.Cols
	rows, curRow, curCol := layoutCursor([]rune(stripANSI(line)), cur, cols)
	var b strings.Builder
	b.WriteString(term.LineStart())
	if e.cursorRow > 0 {
		b.WriteString(term.CursorUp(e.cursorRow))
	}
	b.WriteString(term.ClearToEOL())
	clearRows := e.rowsUsed - 1
	if limit := size.Rows - 1; clearRows > limit {
		clearRows = limit
	}
	for i := 0; i < clearRows; i++ {
		b.WriteString("\r\n" + term.ClearToEOL())
	}
	if clearRows > 0 {
		b.WriteString(term.CursorUp(clearRows))
	}
	b.WriteString(line)
	menu := e.menuLines(cols)
	for _, ml := range menu {
		b.WriteString("\r\n" + ml)
	}
	up := rows - 1 + len(menu) - curRow
	if up > 0 {
		b.WriteString(term.CursorUp(up))
	}
	b.WriteString(term.LineStart())
	if curCol > 0 {
		if curCol >= cols {
			curCol = cols - 1
		}
		b.WriteString(term.CursorForward(curCol))
	}
	e.cursorRow = curRow
	e.rowsUsed = rows + len(menu)
	fmt.Fprint(e.out, b.String())
}
