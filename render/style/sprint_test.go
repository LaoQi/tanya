package style

import "testing"

func TestSprintCombinations(t *testing.T) {
	cases := []struct {
		name  string
		style Style
		text  string
		want  string
	}{
		{"fg", Style{Fg: Color16(1)}, "x", "\x1b[31mx\x1b[0m"},
		{"bright bg", Style{Bg: Color16(12)}, "x", "\x1b[104mx\x1b[0m"},
		{"fg+attr", Style{Fg: Color16(1), Attr: AttrBold}, "x", "\x1b[31;1mx\x1b[0m"},
		{"multi attr", Style{Attr: AttrBold | AttrUnderline}, "x", "\x1b[1;4mx\x1b[0m"},
		{"empty style", Style{}, "x", "x"},
	}
	for _, c := range cases {
		if got := c.style.Sprint(c.text); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
