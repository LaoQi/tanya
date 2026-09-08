package style

import "testing"

func TestSemanticSGR(t *testing.T) {
	cases := []struct {
		name  string
		style Style
		want  string
	}{
		{"dim", Dim, "\x1b[90m"},
		{"info", Info, "\x1b[94m"},
		{"warn", Warn, "\x1b[33m"},
		{"ok", Ok, "\x1b[32m"},
		{"error", Error, "\x1b[91m"},
		{"accent", Accent, "\x1b[7m"},
	}
	for _, c := range cases {
		if got := GetProfile().sgr(c.style); got != c.want {
			t.Errorf("%s sgr = %q, want %q", c.name, got, c.want)
		}
	}
}

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

func TestSprintProfileNone(t *testing.T) {
	defer SetProfile(GetProfile())
	SetProfile(Profile{TTY: false, Colors: LevelNone})
	if got := Dim.Sprint("abc"); got != "abc" {
		t.Errorf("None profile 下应输出纯文本: %q", got)
	}
	if got := Sprint(Info.Text("a"), Span{Text: "b"}); got != "ab" {
		t.Errorf("None profile 混合: %q", got)
	}
}

func TestSprintMixedInlines(t *testing.T) {
	got := Sprint(Dim.Text("▸ "), Ok.Text("ok"), Span{Text: " end"})
	want := "\x1b[90m▸ \x1b[0m\x1b[32mok\x1b[0m end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
