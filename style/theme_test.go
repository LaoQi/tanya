package style

import (
	"reflect"
	"testing"
)

func resetThemeState(t *testing.T) {
	t.Helper()
	ApplyScheme("default")
	ApplyPalette(nil)
	t.Cleanup(func() {
		ApplyPalette(nil)
		ApplyScheme("default")
	})
}

func TestSchemeRegistry(t *testing.T) {
	want := []string{"default", "minimal", "solar", "vivid", "nord", "gruv", "dusk"}
	if got := SchemeNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("SchemeNames = %v, want %v", got, want)
	}
	for _, n := range want {
		if !HasScheme(n) {
			t.Errorf("HasScheme(%q) 应为 true", n)
		}
		if _, ok := LookupScheme(n); !ok {
			t.Errorf("LookupScheme(%q) 应命中", n)
		}
	}
	if HasScheme("bogus") {
		t.Error("非法主题不应命中")
	}
	if _, ok := LookupScheme("bogus"); ok {
		t.Error("LookupScheme(bogus) 不应命中")
	}
}

func TestCurrentSchemeDefaults(t *testing.T) {
	resetThemeState(t)
	if CurrentSchemeName() != "default" {
		t.Errorf("默认主题应为 default: %q", CurrentSchemeName())
	}
	if CurrentScheme().Prompt != DefaultPrompt {
		t.Errorf("default 主题提示符应与 DefaultPrompt 一致: %q", CurrentScheme().Prompt)
	}
}

func TestApplySchemeUpdatesSemantics(t *testing.T) {
	resetThemeState(t)
	s, ok := ApplyScheme("vivid")
	if !ok || s.Name != "vivid" {
		t.Fatalf("ApplyScheme(vivid) 应成功: %v %v", s, ok)
	}
	if CurrentSchemeName() != "vivid" {
		t.Errorf("当前主题应为 vivid: %q", CurrentSchemeName())
	}
	if Info.Fg.V16 != 14 || Warn.Fg.V16 != 11 || Ok.Fg.V16 != 10 || Error.Fg.V16 != 9 || Dim.Fg.V16 != 8 {
		t.Errorf("vivid 语义色未生效: info=%d warn=%d ok=%d error=%d dim=%d",
			Info.Fg.V16, Warn.Fg.V16, Ok.Fg.V16, Error.Fg.V16, Dim.Fg.V16)
	}
	if Accent.Attr&AttrReverse == 0 {
		t.Error("accent 应保持反显")
	}
	if s.MD.Headings[1].Fg.V16 != 13 || s.MD.Headings[1].Attr&AttrBold == 0 {
		t.Errorf("vivid h2 应为亮紫粗体: %+v", s.MD.Headings[1])
	}
}

func TestApplySchemeBadNameKeepsCurrent(t *testing.T) {
	resetThemeState(t)
	ApplyScheme("solar")
	if _, ok := ApplyScheme("bogus"); ok {
		t.Fatal("非法主题不应成功")
	}
	if CurrentSchemeName() != "solar" {
		t.Errorf("失败切换不应改变当前主题: %q", CurrentSchemeName())
	}
}

func TestApplySchemeReplaysPalette(t *testing.T) {
	resetThemeState(t)
	ApplyPalette(map[string]string{"info": "bright_red", "bogus": "green"})
	s, ok := ApplyScheme("solar")
	if !ok {
		t.Fatal("ApplyScheme(solar) 应成功")
	}
	if Info.Fg.V16 != 9 {
		t.Errorf("palette 覆盖应叠加于主题之上: info=%d", Info.Fg.V16)
	}
	if s.Sem.Info.Fg.V16 != 6 {
		t.Errorf("palette 不应修改主题定义: %d", s.Sem.Info.Fg.V16)
	}
}

func TestApplySchemeNilPaletteNoOverride(t *testing.T) {
	resetThemeState(t)
	ApplyPalette(nil)
	ApplyScheme("solar")
	if Info.Fg.V16 != 6 {
		t.Errorf("无 palette 覆盖时信息色应为主题值 cyan(6): %d", Info.Fg.V16)
	}
}

func TestMinimalThemeUnderlineInlineCode(t *testing.T) {
	resetThemeState(t)
	s, ok := LookupScheme("minimal")
	if !ok {
		t.Fatal("minimal 应存在")
	}
	if s.MD.CodeInline.Attr&AttrUnderline == 0 || s.MD.CodeInline.Fg.Kind != KindNone {
		t.Errorf("minimal 行内代码应为纯下划线: %+v", s.MD.CodeInline)
	}
	if s.MD.Headings[0].Fg.V16 != 15 || s.MD.Headings[0].Attr&AttrBold == 0 {
		t.Errorf("minimal h1 应为亮白粗体: %+v", s.MD.Headings[0])
	}
}

func TestNewThemesDistinct(t *testing.T) {
	resetThemeState(t)
	nord, _ := LookupScheme("nord")
	if nord.Sem.Info.Fg.V16 != 12 || nord.MD.Headings[1].Fg.V16 != 14 {
		t.Errorf("nord 应为亮蓝信息 + 冰青 h2: info=%d h2=%d", nord.Sem.Info.Fg.V16, nord.MD.Headings[1].Fg.V16)
	}
	gruv, _ := LookupScheme("gruv")
	if gruv.Sem.Info.Fg.V16 != 3 || gruv.MD.Headings[1].Fg.V16 != 3 {
		t.Errorf("gruv 应为金黄主调: info=%d h2=%d", gruv.Sem.Info.Fg.V16, gruv.MD.Headings[1].Fg.V16)
	}
	dusk, _ := LookupScheme("dusk")
	if dusk.Prompt == "" || dusk.MD.Headings[2].Fg.V16 != 13 {
		t.Errorf("dusk 应含紫灰 h3: h3=%d", dusk.MD.Headings[2].Fg.V16)
	}
	for _, s := range []Scheme{nord, gruv, dusk} {
		if s.Prompt == "" || !HasScheme(s.Name) {
			t.Errorf("主题 %s 提示符不应为空", s.Name)
		}
	}
}
