package theme

import (
	"testing"

	rstyle "github.com/LaoQi/tanya/render/style"
)

func TestSemanticsByName(t *testing.T) {
	sem := Semantics{
		Dim:    rstyle.Style{Fg: rstyle.Color16(1)},
		Info:   rstyle.Style{Fg: rstyle.Color16(2)},
		Warn:   rstyle.Style{Fg: rstyle.Color16(3)},
		Ok:     rstyle.Style{Fg: rstyle.Color16(4)},
		Error:  rstyle.Style{Fg: rstyle.Color16(5)},
		Accent: rstyle.Style{Attr: rstyle.AttrReverse},
		Think:  rstyle.Style{Fg: rstyle.Color16(6)},
		Run:    rstyle.Style{Fg: rstyle.Color16(7)},
	}
	cases := []struct {
		name string
		want rstyle.Style
	}{
		{"dim", sem.Dim},
		{"info", sem.Info},
		{"warn", sem.Warn},
		{"ok", sem.Ok},
		{"error", sem.Error},
		{"accent", sem.Accent},
		{"think", sem.Think},
		{"run", sem.Run},
	}
	seen := make(map[rstyle.Style]string, len(cases))
	for _, c := range cases {
		if prev, dup := seen[c.want]; dup {
			t.Fatalf("用例前提失效: %q 与 %q 期望样式相同 %+v，无法区分映射", c.name, prev, c.want)
		}
		seen[c.want] = c.name
		got, ok := sem.ByName(c.name)
		if !ok {
			t.Errorf("ByName(%q) 应命中", c.name)
			continue
		}
		if got != c.want {
			t.Errorf("ByName(%q) = %+v, want %+v", c.name, got, c.want)
		}
	}
	for _, key := range []string{"bogus", "Dim", "OK", "", "dim "} {
		if got, ok := sem.ByName(key); ok || got != (rstyle.Style{}) {
			t.Errorf("ByName(%q) 应未命中且返回空样式: %+v ok=%v", key, got, ok)
		}
	}
}
