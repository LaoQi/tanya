package term

import (
	"strings"
	"unicode/utf8"
)

const (
	ansiMarker   rune = 0x1b
	truncateTail      = "~"
	Reset             = "\x1b[0m"
)

func runeWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 0x1100:
		return 1
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x231A && r <= 0x231B,
		r >= 0x23E9 && r <= 0x23EC,
		r == 0x23F0,
		r == 0x23F3,
		r >= 0x25FD && r <= 0x25FE,
		r >= 0x2614 && r <= 0x2615,
		r >= 0x2648 && r <= 0x2653,
		r == 0x267F,
		r == 0x2693,
		r == 0x26A1,
		r >= 0x26AA && r <= 0x26AB,
		r >= 0x26BD && r <= 0x26BE,
		r >= 0x26C4 && r <= 0x26C5,
		r == 0x26CE,
		r == 0x26D4,
		r == 0x26EA,
		r >= 0x26F2 && r <= 0x26F3,
		r == 0x26F5,
		r == 0x26FA,
		r == 0x26FD,
		r == 0x2705,
		r >= 0x270A && r <= 0x270B,
		r == 0x2728,
		r == 0x274C,
		r == 0x274E,
		r >= 0x2753 && r <= 0x2755,
		r == 0x2757,
		r >= 0x2795 && r <= 0x2797,
		r == 0x27B0,
		r == 0x27BF,
		r >= 0x2B1B && r <= 0x2B1C,
		r == 0x2B50,
		r == 0x2B55,
		r >= 0x2E80 && r <= 0x303E,
		r >= 0x3041 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xA000 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r == 0x1F004,
		r == 0x1F0CF,
		r == 0x1F18E,
		r >= 0x1F191 && r <= 0x1F19A,
		r >= 0x1F200 && r <= 0x1F2FF,
		r >= 0x1F300 && r <= 0x1F64F,
		r >= 0x1F680 && r <= 0x1F6FF,
		r >= 0x1F900 && r <= 0x1F9FF,
		r >= 0x1FA70 && r <= 0x1FAFF,
		r >= 0x20000 && r <= 0x2FFFD,
		r >= 0x30000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

func Strip(s string) string {
	return Sanitize(s, false)
}

func Width(s string) int {
	n := 0
	i := 0
	for i < len(s) {
		if s[i] == byte(ansiMarker) {
			_, _, next := scanSequence(s, i)
			if next <= i {
				break
			}
			i = next
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		n += runeWidth(r)
		i += size
	}
	return n
}

func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	max := w - runeWidth(rune(truncateTail[0]))
	var b strings.Builder
	b.Grow(len(s) + len(Reset) + len(truncateTail))
	cur := 0
	var sgr strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == byte(ansiMarker) {
			seq, kind, next := scanSequence(s, i)
			if next <= i {
				break
			}
			b.WriteString(seq)
			if kind == seqSGR {
				if seq == Reset || seq == "\x1b[m" {
					sgr.Reset()
				} else {
					sgr.WriteString(seq)
				}
			}
			i = next
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		cur += runeWidth(r)
		if cur > max {
			b.WriteString(truncateTail)
			if sgr.Len() > 0 {
				b.WriteString(Reset)
			}
			return b.String()
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Wrap 把文本折成不超过 w 列的若干行：按显示宽度折行（宽字符不劈开），
// 遇 \n 硬断行（换行符本身不写出）、\r 丢弃，ANSI 序列原样保留且不计宽度。
// 不做词级折行——超长 token 一样按宽度硬切。w <= 0 时返回单行原文。
func Wrap(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	var lines []string
	var b strings.Builder
	cur := 0
	i := 0
	for i < len(s) {
		if s[i] == byte(ansiMarker) {
			seq, _, next := scanSequence(s, i)
			if next <= i {
				break
			}
			b.WriteString(seq)
			i = next
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch r {
		case '\n':
			lines = append(lines, b.String())
			b.Reset()
			cur = 0
			continue
		case '\r':
			continue
		}
		rw := runeWidth(r)
		if cur > 0 && cur+rw > w {
			lines = append(lines, b.String())
			b.Reset()
			cur = 0
		}
		b.WriteRune(r)
		cur += rw
	}
	return append(lines, b.String())
}
