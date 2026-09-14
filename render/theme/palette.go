package theme

import rstyle "github.com/LaoQi/tanyan/render/style"

// Apply 在内置方案语义色之上叠加用户 palette 覆盖，返回新的语义色集合（纯函数）。
func Apply(base Semantics, m map[string]string) Semantics {
	for k, v := range m {
		c, ok := rstyle.ColorByName(v)
		if !ok {
			continue
		}
		switch k {
		case "dim":
			base.Dim = rstyle.Style{Fg: c}
		case "info":
			base.Info = rstyle.Style{Fg: c}
		case "warn":
			base.Warn = rstyle.Style{Fg: c}
		case "ok":
			base.Ok = rstyle.Style{Fg: c}
		case "error":
			base.Error = rstyle.Style{Fg: c}
		case "accent":
			base.Accent = rstyle.Style{Fg: c, Attr: rstyle.AttrReverse}
		case "think":
			base.Think = rstyle.Style{Fg: c}
		case "run":
			base.Run = rstyle.Style{Fg: c}
		}
	}
	return base
}
