package style

import "strings"

type seqKind uint8

const (
	seqNone seqKind = iota
	seqSGR
	seqOther
)

func (s Style) Frame(text string) string {
	clean := sanitize(text, false)
	seq := current.sgr(s)
	if seq == "" {
		return clean
	}
	return seq + clean + resetSequence
}

func Passthrough(text string) string {
	return sanitize(text, current.Colors != LevelNone)
}

func HasSGR(s string) bool {
	i := 0
	for i < len(s) {
		if s[i] != 0x1b {
			i++
			continue
		}
		_, kind, next := scanSequence(s, i)
		if kind == seqSGR {
			return true
		}
		i = next
	}
	return false
}

func sanitize(s string, keepSGR bool) string {
	var b strings.Builder
	b.Grow(len(s))
	dirty := false
	i := 0
	for i < len(s) {
		c := s[i]
		if c != 0x1b {
			switch {
			case c == '\n' || c == '\t':
				b.WriteByte(c)
			case c < 0x20 || c == 0x7f:
			default:
				b.WriteByte(c)
			}
			i++
			continue
		}
		seq, kind, next := scanSequence(s, i)
		if kind == seqSGR && keepSGR {
			b.WriteString(seq)
			dirty = sgrLeavesState(seq)
		}
		i = next
	}
	if dirty {
		b.WriteString(resetSequence)
	}
	return b.String()
}

func scanSequence(s string, i int) (string, seqKind, int) {
	start := i
	if i+1 >= len(s) {
		return s[start:], seqNone, len(s)
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j >= len(s) {
			return s[start:], seqNone, len(s)
		}
		if s[j] == 'm' {
			return s[start : j+1], seqSGR, j + 1
		}
		return s[start : j+1], seqOther, j + 1
	case ']':
		j := i + 2
		for j < len(s) {
			if s[j] == 0x07 {
				return s[start : j+1], seqOther, j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return s[start : j+2], seqOther, j + 2
			}
			j++
		}
		return s[start:], seqNone, len(s)
	case '(', ')', '*', '+', '#', '%':
		if i+2 < len(s) {
			return s[start : i+3], seqOther, i + 3
		}
		return s[start:], seqNone, len(s)
	default:
		return s[start : i+2], seqOther, i + 2
	}
}

func sgrLeavesState(seq string) bool {
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
	if body == "" {
		return false
	}
	parts := strings.Split(body, ";")
	lastReset := false
	for k := 0; k < len(parts); k++ {
		p := parts[k]
		if p == "38" || p == "48" {
			if k+1 >= len(parts) {
				break
			}
			switch parts[k+1] {
			case "5":
				k += 2
			case "2":
				k += 4
			default:
				k++
			}
			lastReset = false
			continue
		}
		lastReset = p == "" || p == "0"
	}
	return !lastReset
}
