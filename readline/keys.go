package readline

import "unicode/utf8"

type KeyCode int

const (
	KeyRune KeyCode = iota
	KeyEnter
	KeyBackspace
	KeyTab
	KeyEsc
	KeyCtrlC
	KeyCtrlD
	KeyCtrlA
	KeyCtrlE
	KeyCtrlB
	KeyCtrlF
	KeyCtrlU
	KeyCtrlK
	KeyCtrlW
	KeyCtrlY
	KeyCtrlT
	KeyCtrlL
	KeyAltB
	KeyAltF
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyDelete
	KeyLine
)

type KeyEvent struct {
	Code KeyCode
	Rune rune
	Text string
}

type keyParser struct {
	buf      []byte
	needMore bool
}

func (p *keyParser) needsMore() bool { return p.needMore }

func (p *keyParser) feed(data []byte) []KeyEvent {
	p.buf = append(p.buf, data...)
	var out []KeyEvent
	for len(p.buf) > 0 {
		ev, n, more := p.parse()
		if more {
			p.needMore = true
			return out
		}
		p.needMore = false
		if n == 0 {
			break
		}
		p.buf = p.buf[n:]
		if ev != nil {
			out = append(out, *ev)
		}
	}
	p.needMore = false
	return out
}

func (p *keyParser) flush() []KeyEvent {
	var out []KeyEvent
	for len(p.buf) > 0 {
		ev, n, more := p.parse()
		if more || n == 0 {
			if p.buf[0] == 0x1b {
				out = append(out, KeyEvent{Code: KeyEsc})
			}
			p.buf = p.buf[1:]
			continue
		}
		p.buf = p.buf[n:]
		if ev != nil {
			out = append(out, *ev)
		}
	}
	p.needMore = false
	return out
}

func (p *keyParser) parse() (*KeyEvent, int, bool) {
	if len(p.buf) == 0 {
		return nil, 0, false
	}
	b := p.buf[0]
	switch {
	case b == 0x1b:
		return p.parseEscape()
	case b == '\r' || b == '\n':
		return &KeyEvent{Code: KeyEnter}, 1, false
	case b == 0x7f || b == 0x08:
		return &KeyEvent{Code: KeyBackspace}, 1, false
	case b == '\t':
		return &KeyEvent{Code: KeyTab}, 1, false
	case b == 0x03:
		return &KeyEvent{Code: KeyCtrlC}, 1, false
	case b == 0x04:
		return &KeyEvent{Code: KeyCtrlD}, 1, false
	case b == 0x01:
		return &KeyEvent{Code: KeyCtrlA}, 1, false
	case b == 0x05:
		return &KeyEvent{Code: KeyCtrlE}, 1, false
	case b == 0x02:
		return &KeyEvent{Code: KeyCtrlB}, 1, false
	case b == 0x06:
		return &KeyEvent{Code: KeyCtrlF}, 1, false
	case b == 0x15:
		return &KeyEvent{Code: KeyCtrlU}, 1, false
	case b == 0x0b:
		return &KeyEvent{Code: KeyCtrlK}, 1, false
	case b == 0x17:
		return &KeyEvent{Code: KeyCtrlW}, 1, false
	case b == 0x19:
		return &KeyEvent{Code: KeyCtrlY}, 1, false
	case b == 0x14:
		return &KeyEvent{Code: KeyCtrlT}, 1, false
	case b == 0x0c:
		return &KeyEvent{Code: KeyCtrlL}, 1, false
	case b < 0x20:
		return nil, 1, false
	case b < 0x80:
		return &KeyEvent{Code: KeyRune, Rune: rune(b)}, 1, false
	}
	r, size := utf8.DecodeRune(p.buf)
	if r == utf8.RuneError && size <= 1 && len(p.buf) < 4 {
		return nil, 0, true
	}
	if r == utf8.RuneError && size <= 1 {
		return nil, 1, false
	}
	return &KeyEvent{Code: KeyRune, Rune: r}, size, false
}

func (p *keyParser) parseEscape() (*KeyEvent, int, bool) {
	if len(p.buf) == 1 {
		return nil, 0, true
	}
	if p.buf[1] != '[' && p.buf[1] != 'O' {
		switch p.buf[1] {
		case 'b':
			return &KeyEvent{Code: KeyAltB}, 2, false
		case 'f':
			return &KeyEvent{Code: KeyAltF}, 2, false
		}
		return &KeyEvent{Code: KeyEsc}, 1, false
	}
	if len(p.buf) == 2 {
		return nil, 0, true
	}
	switch p.buf[2] {
	case 'A':
		return &KeyEvent{Code: KeyUp}, 3, false
	case 'B':
		return &KeyEvent{Code: KeyDown}, 3, false
	case 'C':
		return &KeyEvent{Code: KeyRight}, 3, false
	case 'D':
		return &KeyEvent{Code: KeyLeft}, 3, false
	case 'H':
		return &KeyEvent{Code: KeyHome}, 3, false
	case 'F':
		return &KeyEvent{Code: KeyEnd}, 3, false
	case '1', '4':
		if len(p.buf) < 4 {
			return nil, 0, true
		}
		if p.buf[3] == '~' {
			if p.buf[2] == '1' {
				return &KeyEvent{Code: KeyHome}, 4, false
			}
			return &KeyEvent{Code: KeyEnd}, 4, false
		}
		return nil, 3, false
	case '3':
		if len(p.buf) < 4 {
			return nil, 0, true
		}
		if p.buf[3] == '~' {
			return &KeyEvent{Code: KeyDelete}, 4, false
		}
		return nil, 3, false
	}
	return nil, 3, false
}
