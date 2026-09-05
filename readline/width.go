package readline

import (
	"strings"
	"unicode/utf8"
)

func runeWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 0x1100:
		return 1
	case r >= 0x1100 && r <= 0x115F,
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
		r >= 0x1F300 && r <= 0x1F64F,
		r >= 0x1F900 && r <= 0x1F9FF,
		r >= 0x20000 && r <= 0x2FFFD,
		r >= 0x30000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

func stringWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !((s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z')) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

func stringWidthANSI(s string) int {
	return stringWidth(stripANSI(s))
}

func Truncate(s string, w int) string {
	if stringWidth(s) <= w {
		return s
	}
	var b strings.Builder
	cur := 0
	for _, r := range s {
		rw := runeWidth(r)
		if cur+rw > w-1 {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	b.WriteByte('~')
	return b.String()
}
