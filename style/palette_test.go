package style

import "testing"

func TestApplyPalette(t *testing.T) {
	defer ApplyPalette(map[string]string{
		"dim": "bright_black", "info": "bright_blue", "warn": "yellow",
		"ok": "green", "error": "bright_red",
	})
	ApplyPalette(map[string]string{
		"info":  "bright_red",
		"error": "green",
		"bogus": "red",
		"warn":  "notacolor",
	})
	if Info.Fg.V16 != 9 {
		t.Errorf("info 应更新为 bright_red: %+v", Info)
	}
	if Error.Fg.V16 != 2 {
		t.Errorf("error 应更新为 green: %+v", Error)
	}
	if Dim.Fg.V16 != 8 || Ok.Fg.V16 != 2 {
		t.Error("未涉及的语义色不应变动")
	}
	if Warn.Fg.V16 != 3 {
		t.Errorf("非法色名应忽略: %+v", Warn)
	}
}

func TestApplyPaletteSpinColors(t *testing.T) {
	defer ApplyPalette(nil)
	ApplyPalette(map[string]string{"think": "bright_magenta", "run": "bright_cyan"})
	if Think.Fg.V16 != 13 || Run.Fg.V16 != 14 {
		t.Errorf("think/run palette 覆盖未生效: think=%d run=%d", Think.Fg.V16, Run.Fg.V16)
	}
	ApplyPalette(map[string]string{"think": "notacolor"})
	if Think.Fg.V16 != 13 {
		t.Errorf("非法色名应忽略: %+v", Think)
	}
}
