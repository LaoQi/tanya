package theme

import rstyle "github.com/LaoQi/tanyan/render/style"

func (s Semantics) ByName(name string) (rstyle.Style, bool) {
	switch name {
	case "dim":
		return s.Dim, true
	case "info":
		return s.Info, true
	case "warn":
		return s.Warn, true
	case "ok":
		return s.Ok, true
	case "error":
		return s.Error, true
	case "accent":
		return s.Accent, true
	case "think":
		return s.Think, true
	case "run":
		return s.Run, true
	}
	return rstyle.Style{}, false
}
