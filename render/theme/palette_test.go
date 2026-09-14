package theme

import "testing"

func TestApply(t *testing.T) {
	base := baseSem(t)
	got := Apply(base, map[string]string{
		"info":  "bright_red",
		"error": "green",
		"bogus": "red",
		"warn":  "notacolor",
	})
	if got.Info.Fg.V16 != 9 {
		t.Errorf("info 应更新为 bright_red: %+v", got.Info)
	}
	if got.Error.Fg.V16 != 2 {
		t.Errorf("error 应更新为 green: %+v", got.Error)
	}
	if got.Dim.Fg.V16 != base.Dim.Fg.V16 || got.Ok.Fg.V16 != base.Ok.Fg.V16 {
		t.Error("未涉及的语义色不应变动")
	}
	if got.Warn.Fg.V16 != base.Warn.Fg.V16 {
		t.Errorf("非法色名应忽略: %+v", got.Warn)
	}
	if base.Info.Fg.V16 == 9 {
		t.Error("Apply 不应修改入参")
	}
}

func TestApplySpinColors(t *testing.T) {
	base := baseSem(t)
	got := Apply(base, map[string]string{"think": "bright_magenta", "run": "bright_cyan"})
	if got.Think.Fg.V16 != 13 || got.Run.Fg.V16 != 14 {
		t.Errorf("think/run palette 覆盖未生效: think=%d run=%d", got.Think.Fg.V16, got.Run.Fg.V16)
	}
	got = Apply(got, map[string]string{"think": "notacolor"})
	if got.Think.Fg.V16 != 13 {
		t.Errorf("非法色名应忽略: %+v", got.Think)
	}
}

func baseSem(t *testing.T) Semantics {
	t.Helper()
	s, ok := Lookup("default")
	if !ok {
		t.Fatal("default 方案应存在")
	}
	return s.Sem
}
