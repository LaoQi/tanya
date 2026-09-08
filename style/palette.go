package style

func ParseColorName(name string) (Color, bool) {
	v, ok := colorNames[name]
	if !ok {
		return Color{}, false
	}
	return Color16(v), true
}

func ApplyPalette(m map[string]string) {
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
		}
	}
}
