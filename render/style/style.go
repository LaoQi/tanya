package style

import (
	"strconv"
	"strings"

	"github.com/LaoQi/tanya/render/term"
)

type ColorKind uint8

const (
	KindNone ColorKind = iota
	Kind16
)

type Color struct {
	Kind ColorKind
	V16  uint8
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

func (s Style) Sprint(t string) string {
	seq := s.SGR(term.GetProfile())
	if seq == "" {
		return t
	}
	return seq + t + term.Reset
}

func (s Style) empty() bool {
	return s.Fg.Kind == KindNone && s.Bg.Kind == KindNone && s.Attr == 0
}

func (s Style) SGR(p term.Profile) string {
	if p.Colors == term.LevelNone || s.empty() {
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
