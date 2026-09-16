package term

import "testing"

func TestDetectProfile(t *testing.T) {
	cases := []struct {
		name   string
		isTTY  bool
		vt     bool
		env    map[string]string
		colors ColorLevel
		tty    bool
	}{
		{name: "tty 默认 16 色", isTTY: true, vt: true, colors: Level16, tty: true},
		{name: "非 tty 无色", colors: LevelNone},
		{name: "VT 不可用无色", isTTY: true, colors: LevelNone, tty: true},
		{name: "NO_COLOR", isTTY: true, vt: true, env: map[string]string{"NO_COLOR": "1"}, colors: LevelNone, tty: true},
		{name: "强制开色", vt: true, env: map[string]string{"TANYA_COLOR": "1"}, colors: Level16},
		{name: "VT 不可用下强制开色", isTTY: true, env: map[string]string{"TANYA_COLOR": "1"}, colors: Level16, tty: true},
		{name: "强制关色", isTTY: true, vt: true, env: map[string]string{"TANYA_COLOR": "0"}, colors: LevelNone, tty: true},
		{name: "256 色终端按 16 色渲染", isTTY: true, vt: true, env: map[string]string{"TERM": "xterm-256color"}, colors: Level16, tty: true},
		{name: "truecolor 终端按 16 色渲染", isTTY: true, vt: true, env: map[string]string{"COLORTERM": "truecolor"}, colors: Level16, tty: true},
		{name: "dumb", isTTY: true, vt: true, env: map[string]string{"TERM": "dumb"}, colors: LevelNone, tty: true},
		{name: "dumb 下强制开色恢复 16 色", isTTY: true, vt: true, env: map[string]string{"TERM": "dumb", "TANYA_COLOR": "1"}, colors: Level16, tty: true},
	}
	for _, c := range cases {
		termEnv := c.env["TERM"]
		if termEnv == "" {
			termEnv = "xterm"
		}
		t.Setenv("TERM", termEnv)
		t.Setenv("NO_COLOR", c.env["NO_COLOR"])
		t.Setenv("TANYA_COLOR", c.env["TANYA_COLOR"])
		t.Setenv("COLORTERM", c.env["COLORTERM"])
		t.Setenv("WT_SESSION", c.env["WT_SESSION"])
		p := DetectProfile(c.isTTY, c.vt)
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
