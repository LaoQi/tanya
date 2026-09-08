package style

import (
	"strconv"
	"strings"
)

type ColorKind uint8

const (
	KindNone ColorKind = iota
	Kind16
	KindRGB
)

type Color struct {
	Kind ColorKind
	V16  uint8
	RGB  uint32
}

func Color16(v uint8) Color {
	return Color{Kind: Kind16, V16: v}
}

type Attr uint8

const (
	AttrBold Attr = 1 << iota
	AttrUnderline
	AttrReverse
	AttrItalic
)

type Style struct {
	Fg   Color
	Bg   Color
	Attr Attr
}

var (
	Dim    = Style{Fg: Color16(8)}
	Info   = Style{Fg: Color16(12)}
	Warn   = Style{Fg: Color16(3)}
	Ok     = Style{Fg: Color16(2)}
	Error  = Style{Fg: Color16(9)}
	Accent = Style{Attr: AttrReverse}
)

func (s Style) Text(t string) Span {
	return Span{Style: s, Text: t}
}

func (s Style) Sprint(t string) string {
	return Sprint(s.Text(t))
}

func (s Style) empty() bool {
	return s.Fg.Kind == KindNone && s.Bg.Kind == KindNone && s.Attr == 0
}

func (p Profile) sgr(s Style) string {
	if p.Colors == LevelNone || s.empty() {
		return ""
	}
	var params []string
	if s.Fg.Kind == Kind16 {
		params = append(params, fgSeq(s.Fg.V16))
	}
	if s.Bg.Kind == Kind16 {
		params = append(params, bgSeq(s.Bg.V16))
	}
	if s.Attr&AttrBold != 0 {
		params = append(params, "1")
	}
	if s.Attr&AttrUnderline != 0 {
		params = append(params, "4")
	}
	if s.Attr&AttrReverse != 0 {
		params = append(params, "7")
	}
	if s.Attr&AttrItalic != 0 {
		params = append(params, "3")
	}
	if len(params) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(params, ";") + "m"
}

func fgSeq(v uint8) string {
	if v < 8 {
		return strconv.Itoa(30 + int(v))
	}
	return strconv.Itoa(90 + int(v) - 8)
}

func bgSeq(v uint8) string {
	if v < 8 {
		return strconv.Itoa(40 + int(v))
	}
	return strconv.Itoa(100 + int(v) - 8)
}
