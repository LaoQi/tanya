package term

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

func TestStripOSCAndNonCSISequences(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\x1b]0;my-title\x07hello", "hello"},
		{"\x1b]8;;http://x\x1b\\link", "link"},
		{"a\x1b]2;t\x07b\x1b[2Kc", "abc"},
		{"\x1b(Besc", "esc"},
	}
	for _, c := range cases {
		if got := Strip(c.in); got != c.want {
			t.Errorf("Strip(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWidthOSC(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"\x1b]0;my-title\x07hello", 5},
		{"\x1b]8;;http://x\x1b\\link", 4},
		{"\x1b[2Jhello", 5},
	}
	for _, c := range cases {
		if got := Width(c.in); got != c.want {
			t.Errorf("Width(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncateOSC(t *testing.T) {
	in := "\x1b]0;my-title\x07hello"
	if got := Truncate(in, 8); got != in {
		t.Errorf("宽度足够时不应截断: %q", got)
	}
	got := Truncate(in, 4)
	if !strings.HasSuffix(got, "~") || strings.Contains(got, "hello") {
		t.Errorf("超宽时应按可见宽度截断: %q", got)
	}
}

func TestWrap(t *testing.T) {
	cases := []struct {
		name string
		in   string
		w    int
		want []string
	}{
		{"空串", "", 10, []string{""}},
		{"宽度非正", "abc", 0, []string{"abc"}},
		{"恰好整宽", "abcde", 5, []string{"abcde"}},
		{"超宽折行", "abcdefg", 3, []string{"abc", "def", "g"}},
		{"宽字符不劈开", "中中中", 5, []string{"中中", "中"}},
		{"宽字符行内混排", "a中b中c", 4, []string{"a中b", "中c"}},
		{"单字符超宽也自占一行", "中中", 1, []string{"中", "中"}},
		{"硬断行不写出换行符", "a\nbb\nccc", 10, []string{"a", "bb", "ccc"}},
		{"硬断行与折行叠加", "abcdef\ngh", 3, []string{"abc", "def", "gh"}},
		{"丢掉回车", "ab\rcd", 10, []string{"abcd"}},
		{"保留 ANSI 且不计宽", "\x1b[31mabc\x1b[0m", 3, []string{"\x1b[31mabc\x1b[0m"}},
		{"折行不跨行复制 SGR", "\x1b[31mabcdef\x1b[0m", 3, []string{"\x1b[31mabc", "def\x1b[0m"}},
		{"OSC 不计宽", "\x1b]0;title\x07abc", 3, []string{"\x1b]0;title\x07abc"}},
		{"每行都必须是整行", "aaaaaaaa", 4, []string{"aaaa", "aaaa"}},
	}
	for _, c := range cases {
		got := Wrap(c.in, c.w)
		if len(got) != len(c.want) {
			t.Errorf("%s: Wrap(%q, %d) = %q, want %q", c.name, c.in, c.w, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: 第 %d 行 = %q, want %q（全部 %q）", c.name, i, got[i], c.want[i], got)
				break
			}
		}
	}
}

// TestWrapLinesNeverExceedWidth 锁住折行的硬约束：可见宽度不超 w（宽字符整字换行时允许留空）。
func TestWrapLinesNeverExceedWidth(t *testing.T) {
	inputs := []string{
		"for f in *.go; do echo \"$f\"; done",
		strings.Repeat("中", 20),
		"中a中b中c中d中e中f",
		"journalctl -u tanyan --since '2 hours ago' | grep -Ei 'error|warn'",
		"\x1b[32mecho\x1b[0m " + strings.Repeat("x", 40),
	}
	for _, in := range inputs {
		for _, w := range []int{1, 2, 3, 4, 7, 8, 39, 80} {
			for _, line := range Wrap(in, w) {
				// 宽字符整字换行：w 比一个宽字符还窄时该行必然占 2 列，是终端 deferred autowrap 的固有结果。
				if got := Width(line); got > w && !(w == 1 && got == 2) {
					t.Errorf("Wrap(%q, %d) 行宽 %d 越界: %q", in, w, got, line)
				}
			}
			if joined := Strip(strings.Join(Wrap(in, w), "")); joined != Strip(strings.ReplaceAll(in, "\n", "")) {
				t.Errorf("Wrap(%q, %d) 丢字符: %q", in, w, joined)
			}
		}
	}
}
