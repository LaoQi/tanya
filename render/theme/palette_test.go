package theme

import (
	"testing"

	rstyle "github.com/LaoQi/tanyan/render/style"
)

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

func TestApplyRemainingKeys(t *testing.T) {
	base := baseSem(t)
	prev := base
	got := Apply(base, map[string]string{
		"dim":    "bright_white",
		"warn":   "bright_yellow",
		"ok":     "bright_green",
		"accent": "magenta",
	})
	if got.Dim.Fg.V16 != 15 || got.Warn.Fg.V16 != 11 || got.Ok.Fg.V16 != 10 {
		t.Errorf("dim/warn/ok palette 覆盖未生效: dim=%d warn=%d ok=%d",
			got.Dim.Fg.V16, got.Warn.Fg.V16, got.Ok.Fg.V16)
	}
	if got.Accent.Fg.V16 != 5 || got.Accent.Attr&rstyle.AttrReverse == 0 {
		t.Errorf("accent 覆盖应设为 magenta 并保留反显: %+v", got.Accent)
	}
	if base != prev {
		t.Errorf("Apply 不应修改入参: %+v -> %+v", prev, base)
	}

	withFg := Semantics{Accent: rstyle.Style{Fg: rstyle.Color16(3), Attr: rstyle.AttrBold | rstyle.AttrReverse}}
	over := Apply(withFg, map[string]string{"accent": "magenta"})
	if over.Accent.Fg.V16 != 5 || over.Accent.Attr != rstyle.AttrReverse {
		t.Errorf("accent 覆盖应重建样式、清除旧 Fg/Attr: %+v", over.Accent)
	}
}

func TestApplyKeyMatching(t *testing.T) {
	base := baseSem(t)
	got := Apply(base, map[string]string{
		"Dim": "bright_white",
		"OK":  "bright_green",
		"":    "bright_red",
	})
	if got != base {
		t.Errorf("键名应大小写敏感且忽略空键: %+v", got)
	}
}
