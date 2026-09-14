package markup

import (
	"github.com/LaoQi/tanyan/render/ir"
	rstyle "github.com/LaoQi/tanyan/render/style"
	"github.com/LaoQi/tanyan/render/theme"
	"strings"
	"unicode/utf8"
)

var attrNames = map[string]rstyle.Attr{
	"bold": rstyle.AttrBold, "underline": rstyle.AttrUnderline, "reverse": rstyle.AttrReverse,
}

func parseStyleNames(inner string, sem theme.Semantics) (rstyle.Style, bool) {
	var st rstyle.Style
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return st, false
	}
	for _, f := range fields {
		if a, ok := attrNames[f]; ok {
			st.Attr |= a
			continue
		}
		if c, ok := rstyle.ColorByName(f); ok {
			st.Fg = c
			continue
		}
		if s, ok := sem.ByName(f); ok {
			if s.Fg.Kind != rstyle.KindNone {
				st.Fg = s.Fg
			}
			st.Attr |= s.Attr
			continue
		}
		return rstyle.Style{}, false
	}
	return st, true
}

func mergeStyle(base, add rstyle.Style) rstyle.Style {
	out := base
	if add.Fg.Kind != rstyle.KindNone {
		out.Fg = add.Fg
	}
	if add.Bg.Kind != rstyle.KindNone {
		out.Bg = add.Bg
	}
	out.Attr |= add.Attr
	return out
}

func ParseMarkup(src string, sem theme.Semantics) []ir.Inline {
	var out []ir.Inline
	var buf strings.Builder
	var stack []rstyle.Style
	cur := func() rstyle.Style {
		if len(stack) == 0 {
			return rstyle.Style{}
		}
		return stack[len(stack)-1]
	}
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, ir.Span{Style: cur(), Text: buf.String()})
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
			if st, ok := parseStyleNames(inner, sem); ok {
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
