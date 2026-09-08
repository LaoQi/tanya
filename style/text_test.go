package style

import (
	"strings"
	"testing"
)

func TestStrip(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"a\x1b[1mb\x1b[0;32mc\x1b[m", "abc"},
		{"\x1b[2Kclear\x1b[1Aup", "clearup"},
		{"esc\x1b(Bcharset", "esccharset"},
		{"tail-esc\x1b", "tail-esc"},
		{"unterminated\x1b[3", "unterminated"},
		{"lone\x1bx", "lone"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Strip(c.in); got != c.want {
			t.Errorf("Strip(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"hello", 5},
		{"你好", 4},
		{"a你b", 4},
		{"\x1b[31mred\x1b[0m", 3},
		{"\x1b[1;94m状态\x1b[0m", 4},
		{"\x1b[2K\x1b[31mabc", 3},
		{"trailing\x1b[3", 8},
		{"trailing\x1b", 8},
		{"✅", 2},
		{"🚀", 2},
		{"⚠", 1},
		{"", 0},
	}
	for _, c := range cases {
		if got := Width(c.in); got != c.want {
			t.Errorf("Width(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncatePlain(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"hello world", 11, "hello world"},
		{"hello world", 12, "hello world"},
		{"hello world", 8, "hello w~"},
		{"hello world", 5, "hell~"},
		{"hello", 1, "~"},
		{"hello", 0, ""},
		{"hello", -1, ""},
		{"你好世界", 5, "你好~"},
		{"你好世界", 4, "你~"},
		{"你好世界", 9, "你好世界"},
	}
	for _, c := range cases {
		if got := Truncate(c.in, c.w); got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

func TestTruncateANSI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		w    int
		want string
	}{
		{"fits unchanged", "\x1b[31mabc\x1b[0m", 3, "\x1b[31mabc\x1b[0m"},
		{"cut mid-style appends reset", "\x1b[31mhello world\x1b[0m", 8, "\x1b[31mhello w~\x1b[0m"},
		{"cut after reset no extra", "\x1b[31mabc\x1b[0mhello world", 7, "\x1b[31mabc\x1b[0mhel~"},
		{"multiple sgr accumulated", "\x1b[1m\x1b[31mabcdef\x1b[0m", 5, "\x1b[1m\x1b[31mabcd~\x1b[0m"},
		{"drop escapes after cut", "\x1b[32mgreen\x1b[0m and more", 9, "\x1b[32mgreen\x1b[0m an~"},
		{"non-sgr not in lastseq", "\x1b[2K\x1b[31mabcdef\x1b[0m", 4, "\x1b[2K\x1b[31mabc~\x1b[0m"},
		{"bare reset variant", "\x1b[31mab\x1b[mcdefgh", 6, "\x1b[31mab\x1b[mcde~"},
		{"menu item", "  \x1b[7m/sessions\x1b[0m", 10, "  \x1b[7m/sessio~\x1b[0m"},
		{"escapes only no cut", "\x1b[2K\x1b[1A", 80, "\x1b[2K\x1b[1A"},
		{"wide char boundary", "\x1b[36m中文内容测试\x1b[0m", 7, "\x1b[36m中文内~\x1b[0m"},
	}
	for _, c := range cases {
		if got := Truncate(c.in, c.w); got != c.want {
			t.Errorf("%s: Truncate(%q, %d) = %q, want %q", c.name, c.in, c.w, got, c.want)
		}
	}
}

func TestTruncateWidthInvariant(t *testing.T) {
	inputs := []string{
		"\x1b[31mhello\x1b[0m world 中文测试 \x1b[1mbold\x1b[0m tail",
		"▸ run_shell git log --oneline --graph --all | head -20",
		"  ↳ exit 0 · 0.3s · 12 行 · 共 100 行",
	}
	for _, in := range inputs {
		for w := 1; w <= Width(in)+2; w++ {
			got := Truncate(in, w)
			if gw := Width(got); gw > w {
				t.Errorf("Truncate(%q, %d) = %q, width %d > %d", in, w, got, gw, w)
			}
		}
	}
}

func TestTruncateBalancesSGR(t *testing.T) {
	in := "\x1b[94m  ↳ exit 0 · 300ms · 1 行\x1b[0m"
	for w := 4; w <= Width(in); w++ {
		got := Truncate(in, w)
		if n := strings.Count(got, "\x1b["); strings.Count(got, "m") < n {
			t.Errorf("unbalanced sequences at w=%d: %q", w, got)
		}
	}
}
