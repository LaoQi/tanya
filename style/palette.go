package style

var userPalette map[string]string

// ApplyPalette 设置用户语义色覆盖并立即应用；m 为 nil 时清空覆盖。
// 覆盖叠加在当前主题之上，ApplyScheme 切换主题后自动重放。
func ApplyPalette(m map[string]string) {
	userPalette = m
	applySemanticPalette(m)
}

func ParseColorName(name string) (Color, bool) {
	v, ok := colorNames[name]
	if !ok {
		return Color{}, false
	}
	return Color16(v), true
}

func applySemanticPalette(m map[string]string) {
	for k, v := range m {
		c, ok := ParseColorName(v)
		if !ok {
			continue
		}
		switch k {
		case "dim":
			Dim = Style{Fg: c}
		case "info":
			Info = Style{Fg: c}
		case "warn":
			Warn = Style{Fg: c}
		case "ok":
			Ok = Style{Fg: c}
		case "error":
			Error = Style{Fg: c}
		case "accent":
			Accent = Style{Fg: c, Attr: AttrReverse}
		case "think":
			Think = Style{Fg: c}
		case "run":
			Run = Style{Fg: c}
		}
	}
}
