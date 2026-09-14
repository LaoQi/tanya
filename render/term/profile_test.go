package term

import "testing"

func TestDetectProfile(t *testing.T) {
	cases := []struct {
		name   string
		isTTY  bool
		env    map[string]string
		colors ColorLevel
		tty    bool
	}{
		{"tty 默认 16 色", true, nil, Level16, true},
		{"非 tty 无色", false, nil, LevelNone, false},
		{"NO_COLOR", true, map[string]string{"NO_COLOR": "1"}, LevelNone, true},
		{"强制开色", false, map[string]string{"TANYA_COLOR": "1"}, Level16, false},
		{"强制关色", true, map[string]string{"TANYA_COLOR": "0"}, LevelNone, true},
		{"256 色终端按 16 色渲染", true, map[string]string{"TERM": "xterm-256color"}, Level16, true},
		{"truecolor 终端按 16 色渲染", true, map[string]string{"COLORTERM": "truecolor"}, Level16, true},
		{"dumb", true, map[string]string{"TERM": "dumb"}, LevelNone, true},
		{"dumb 下强制开色恢复 16 色", true, map[string]string{"TERM": "dumb", "TANYA_COLOR": "1"}, Level16, true},
	}
	for _, c := range cases {
		term := c.env["TERM"]
		if term == "" {
			term = "xterm"
		}
		t.Setenv("TERM", term)
		t.Setenv("NO_COLOR", c.env["NO_COLOR"])
		t.Setenv("TANYA_COLOR", c.env["TANYA_COLOR"])
		t.Setenv("COLORTERM", c.env["COLORTERM"])
		t.Setenv("WT_SESSION", c.env["WT_SESSION"])
		p := DetectProfile(c.isTTY)
		if p.Colors != c.colors || p.TTY != c.tty {
			t.Errorf("%s: got %+v, want colors=%d tty=%v", c.name, p, c.colors, c.tty)
		}
	}
}

func TestDefaultProfile(t *testing.T) {
	p := GetProfile()
	if !p.TTY || p.Colors != Level16 {
		t.Errorf("默认 profile 应为彩色 TTY: %+v", p)
	}
}
