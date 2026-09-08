package style

import (
	"strings"
	"unicode/utf8"
)

type Template struct {
	passthrough bool
	raw         string
	inlines     []Inline
}

func ParseTemplate(src string) (Template, error) {
	if strings.Contains(src, "\x1b") {
		return Template{passthrough: true, raw: src}, nil
	}
	return Template{inlines: ParseMarkup(src)}, nil
}

func (t Template) Bind(resolve func(string) (string, bool)) []Inline {
	return bindInlines(t.inlines, resolve)
}

func (t Template) Render(resolve func(string) (string, bool)) string {
	if t.passthrough {
		out := replacePlaceholders(t.raw, resolve)
		if GetProfile().Colors == LevelNone {
			return Strip(out)
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

func bindInlines(in []Inline, resolve func(string) (string, bool)) []Inline {
	var out []Inline
	for _, i := range in {
		sp, ok := i.(Span)
		if !ok {
			out = append(out, i)
			continue
		}
		for _, seg := range scanSegments(sp.Text) {
			if seg.ph {
				v, ok := resolve(seg.text)
				switch {
				case ok && v != "":
					out = append(out, Span{Style: sp.Style, Text: v})
				case !ok:
					out = append(out, Span{Style: sp.Style, Text: "{" + seg.text + "}"})
				}
				continue
			}
			out = append(out, Span{Style: sp.Style, Text: seg.text})
		}
	}
	return out
}
