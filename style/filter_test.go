package style

import "testing"

func setTestLevel(l ColorLevel) func() {
	old := current
	current = Profile{TTY: l != LevelNone, Colors: l, Unicode: true}
	return func() { current = old }
}

func TestFrameStripsAndWraps(t *testing.T) {
	defer setTestLevel(Level16)()
	in := "a\x1b[31m红\x1b[0mb\x1b[2Kc\rd\x1b]0;t\x07e\tf"
	want := "\x1b[90m" + "a红bcde\tf" + "\x1b[0m"
	if got := Dim.Frame(in); got != want {
		t.Errorf("Frame = %q, want %q", got, want)
	}
}

func TestFrameNoColorProfile(t *testing.T) {
	defer setTestLevel(LevelNone)()
	if got := Dim.Frame("a\x1b[31mb\x1b[0m"); got != "ab" {
		t.Errorf("无色环境应退化为纯清洗: %q", got)
	}
}

func TestFrameEmptyStyle(t *testing.T) {
	defer setTestLevel(Level16)()
	if got := (Style{}).Frame("a\x1b[31mb"); got != "ab" {
		t.Errorf("空样式不包裹: %q", got)
	}
}

func TestPassthroughKeepsSGRDropsRest(t *testing.T) {
	defer setTestLevel(Level16)()
	in := "a\x1b[31m红\x1b[0mb\x1b[2K\x1b]0;t\x07c\rd"
	want := "a\x1b[31m红\x1b[0mbcd"
	if got := Passthrough(in); got != want {
		t.Errorf("Passthrough = %q, want %q", got, want)
	}
}

func TestPassthroughClosesDirty(t *testing.T) {
	defer setTestLevel(Level16)()
	if got := Passthrough("a\x1b[31mb"); got != "a\x1b[31mb\x1b[0m" {
		t.Errorf("脏状态结尾应补 reset: %q", got)
	}
}

func TestPassthroughResetTracking(t *testing.T) {
	defer setTestLevel(Level16)()
	cases := []struct {
		in   string
		want string
	}{
		{"\x1b[1;31ma\x1b[0m", "\x1b[1;31ma\x1b[0m"},
		{"a\x1b[1;0mb", "a\x1b[1;0mb"},
		{"a\x1b[0;31mb", "a\x1b[0;31mb\x1b[0m"},
		{"\x1b[ma", "\x1b[ma"},
		{"\x1b[38;2;0;0;0ma", "\x1b[38;2;0;0;0ma\x1b[0m"},
		{"\x1b[38;5;196ma", "\x1b[38;5;196ma\x1b[0m"},
	}
	for _, c := range cases {
		if got := Passthrough(c.in); got != c.want {
			t.Errorf("Passthrough(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPassthroughNoColorDropsSGR(t *testing.T) {
	defer setTestLevel(LevelNone)()
	if got := Passthrough("a\x1b[31mb"); got != "ab" {
		t.Errorf("无色环境应剥色: %q", got)
	}
}

func TestPassthroughKeepsTabNewline(t *testing.T) {
	defer setTestLevel(Level16)()
	if got := Passthrough("a\tb\nc"); got != "a\tb\nc" {
		t.Errorf("tab 与换行应保留: %q", got)
	}
}

func TestPassthroughUnterminated(t *testing.T) {
	defer setTestLevel(Level16)()
	if got := Passthrough("a\x1b[31"); got != "a" {
		t.Errorf("未闭合序列应丢弃: %q", got)
	}
}

func TestHasSGR(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"plain", false},
		{"\x1b[31m", true},
		{"\x1b[0m", true},
		{"\x1b[m", true},
		{"\x1b[1;32m", true},
		{"\x1b[2K", false},
		{"\x1b[1A", false},
		{"\x1b]0;t\x07", false},
		{"a\rb", false},
	}
	for _, c := range cases {
		if got := HasSGR(c.in); got != c.want {
			t.Errorf("HasSGR(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestOneLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\nb", "a b"},
		{"a\r\nb", "a b"},
		{"a\r\x1b[K\x1b[33m等待\x1b[0m\r\x1b[Kpong", "a 等待 pong"},
		{"a\x1b[31m红\x1b[0mb", "a红b"},
		{"a\x1b[33mb", "ab"},
		{"a\x1b]0;title\x07b", "ab"},
		{"a\x07b\tc", "ab c"},
		{"  前导尾随  ", "前导尾随"},
		{"中文标签", "中文标签"},
		{"", ""},
	}
	for _, c := range cases {
		if got := OneLine(c.in); got != c.want {
			t.Errorf("OneLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
