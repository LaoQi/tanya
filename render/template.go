package render

import (
	"github.com/LaoQi/tanya/render/ir"
	"github.com/LaoQi/tanya/render/markup"
	"github.com/LaoQi/tanya/render/theme"
	"strings"
	"unicode/utf8"

	"github.com/LaoQi/tanya/render/term"
)

type Template struct {
	passthrough bool
	raw         string
	inlines     []ir.Inline
}

func ParseTemplate(src string, sem theme.Semantics) (Template, error) {
	if strings.Contains(src, "\x1b") {
		return Template{passthrough: true, raw: src}, nil
	}
	return Template{inlines: markup.ParseMarkup(src, sem)}, nil
}

func (t Template) Bind(resolve func(string) (string, bool)) []ir.Inline {
	return bindInlines(t.inlines, resolve)
}

func (t Template) Render(resolve func(string) (string, bool)) string {
	if t.passthrough {
		out := replacePlaceholders(t.raw, resolve)
		if term.GetProfile().Colors == term.LevelNone {
			return term.Strip(out)
		}
		return out
	}
	return Sprint(t.Bind(resolve)...)
}

type segment struct {
	ph   bool
	text string
}

func scanSegments(s string) []segment {
	var segs []segment
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			segs = append(segs, segment{text: buf.String()})
			buf.Reset()
		}
	}
	i := 0
	for i < len(s) {
		if s[i] == '{' {
			end := strings.IndexByte(s[i:], '}')
			if end > 1 {
				name := s[i+1 : i+end]
				if isPlaceholderName(name) {
					flush()
					segs = append(segs, segment{ph: true, text: name})
					i += end + 1
					continue
				}
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		buf.WriteRune(r)
		i += size
	}
	flush()
	return segs
}

func isPlaceholderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func replacePlaceholders(s string, resolve func(string) (string, bool)) string {
	segs := scanSegments(s)
	if len(segs) == 0 {
		return s
	}
	var b strings.Builder
	for _, seg := range segs {
		if !seg.ph {
			b.WriteString(seg.text)
			continue
		}
		if v, ok := resolve(seg.text); ok {
			b.WriteString(v)
		} else {
			b.WriteString("{" + seg.text + "}")
		}
	}
	return b.String()
}

func bindInlines(in []ir.Inline, resolve func(string) (string, bool)) []ir.Inline {
	var out []ir.Inline
	for _, i := range in {
		sp, ok := i.(ir.Span)
		if !ok {
			out = append(out, i)
			continue
		}
		for _, seg := range scanSegments(sp.Text) {
			if seg.ph {
				v, ok := resolve(seg.text)
				switch {
				case ok && v != "":
					out = append(out, ir.Span{Style: sp.Style, Text: v})
				case !ok:
					out = append(out, ir.Span{Style: sp.Style, Text: "{" + seg.text + "}"})
				}
				continue
			}
			out = append(out, ir.Span{Style: sp.Style, Text: seg.text})
		}
	}
	return out
}
