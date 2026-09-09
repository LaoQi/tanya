package style

import (
	"strings"
	"unicode/utf8"
)

var colorNames = map[string]uint8{
	"black": 0, "red": 1, "green": 2, "yellow": 3,
	"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	"bright_black": 8, "bright_red": 9, "bright_green": 10, "bright_yellow": 11,
	"bright_blue": 12, "bright_magenta": 13, "bright_cyan": 14, "bright_white": 15,
}

var attrNames = map[string]Attr{
	"bold": AttrBold, "underline": AttrUnderline, "reverse": AttrReverse,
}

func semanticByName(name string) (Style, bool) {
	switch name {
	case "dim":
		return Dim, true
	case "info":
		return Info, true
	case "warn":
		return Warn, true
	case "ok":
		return Ok, true
	case "error":
		return Error, true
	case "accent":
		return Accent, true
	case "think":
		return Think, true
	case "run":
		return Run, true
	}
	return Style{}, false
}

func parseStyleNames(inner string) (Style, bool) {
	var st Style
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return st, false
	}
	for _, f := range fields {
		if a, ok := attrNames[f]; ok {
			st.Attr |= a
			continue
		}
		if v, ok := colorNames[f]; ok {
			st.Fg = Color16(v)
			continue
		}
		if s, ok := semanticByName(f); ok {
			if s.Fg.Kind != KindNone {
				st.Fg = s.Fg
			}
			st.Attr |= s.Attr
			continue
		}
		return Style{}, false
	}
	return st, true
}

func mergeStyle(base, add Style) Style {
	out := base
	if add.Fg.Kind != KindNone {
		out.Fg = add.Fg
	}
	if add.Bg.Kind != KindNone {
		out.Bg = add.Bg
	}
	out.Attr |= add.Attr
	return out
}

func ParseMarkup(src string) []Inline {
	var out []Inline
	var buf strings.Builder
	var stack []Style
	cur := func() Style {
		if len(stack) == 0 {
			return Style{}
		}
		return stack[len(stack)-1]
	}
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Span{Style: cur(), Text: buf.String()})
			buf.Reset()
		}
	}
	literal := func(tag string) {
		buf.WriteString(tag)
	}
	i := 0
	for i < len(src) {
		if src[i] == '[' {
			end := strings.IndexByte(src[i:], ']')
			if end < 0 {
				buf.WriteString(src[i:])
				break
			}
			inner := src[i+1 : i+end]
			tag := src[i : i+end+1]
			if inner == "/" {
				if len(stack) > 0 {
					flush()
					stack = stack[:len(stack)-1]
				} else {
					literal(tag)
				}
				i += end + 1
				continue
			}
			if st, ok := parseStyleNames(inner); ok {
				flush()
				stack = append(stack, mergeStyle(cur(), st))
				i += end + 1
				continue
			}
			literal(tag)
			i += end + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(src[i:])
		buf.WriteRune(r)
		i += size
	}
	flush()
	return out
}
