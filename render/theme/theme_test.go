package theme

import (
	"reflect"
	"strings"
	"testing"

	rstyle "github.com/LaoQi/tanyan/render/style"
)

func mustLookup(t *testing.T, name string) Scheme {
	t.Helper()
	s, ok := Lookup(name)
	if !ok {
		t.Fatalf("Lookup(%q) 应命中", name)
	}
	return s
}

func TestSchemeRegistry(t *testing.T) {
	want := []string{"default", "minimal", "solar", "vivid", "nord", "gruv", "dusk"}
	if got := Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
	for _, n := range want {
		if !Has(n) {
			t.Errorf("Has(%q) 应为 true", n)
		}
		s, ok := Lookup(n)
		if !ok {
			t.Errorf("Lookup(%q) 应命中", n)
			continue
		}
		if s.Sem.Think.Fg.Kind == rstyle.KindNone || s.Sem.Run.Fg.Kind == rstyle.KindNone {
			t.Errorf("主题 %s 缺少 think/run 语义色: %+v", n, s.Sem)
		}
		if s.Sem.Run == s.Sem.Ok {
			t.Errorf("主题 %s 执行色不应与成功色相同: %+v", n, s.Sem.Run)
		}
	}
	if Has("bogus") {
		t.Error("非法主题不应命中")
	}
	if _, ok := Lookup("bogus"); ok {
		t.Error("Lookup(bogus) 不应命中")
	}
}

func TestDefaultPromptPlaceholders(t *testing.T) {
	if !strings.Contains(DefaultPrompt, "{cwd}") || !strings.Contains(DefaultPrompt, "{stat}") {
		t.Errorf("默认 prompt 模板异常: %q", DefaultPrompt)
	}
}

func TestDefaultSchemePrompt(t *testing.T) {
	if s := mustLookup(t, "default"); s.Prompt != DefaultPrompt {
		t.Errorf("default 提示符应与 DefaultPrompt 一致: %q", s.Prompt)
	}
}

func TestVividSemantics(t *testing.T) {
	s := mustLookup(t, "vivid")
	sem := s.Sem
	if sem.Info.Fg.V16 != 14 || sem.Warn.Fg.V16 != 11 || sem.Ok.Fg.V16 != 10 || sem.Error.Fg.V16 != 9 || sem.Dim.Fg.V16 != 8 {
		t.Errorf("vivid 语义色异常: info=%d warn=%d ok=%d error=%d dim=%d",
			sem.Info.Fg.V16, sem.Warn.Fg.V16, sem.Ok.Fg.V16, sem.Error.Fg.V16, sem.Dim.Fg.V16)
	}
	if sem.Accent.Attr&rstyle.AttrReverse == 0 {
		t.Error("accent 应保持反显")
	}
	if sem.Think.Fg.V16 != 5 || sem.Run.Fg.V16 != 6 {
		t.Errorf("vivid 思考/执行色异常: think=%d run=%d", sem.Think.Fg.V16, sem.Run.Fg.V16)
	}
	if s.MD.Headings[1].Fg.V16 != 13 || s.MD.Headings[1].Attr&rstyle.AttrBold == 0 {
		t.Errorf("vivid h2 应为亮紫粗体: %+v", s.MD.Headings[1])
	}
}

func TestApplyReplaysPaletteOverScheme(t *testing.T) {
	s := mustLookup(t, "solar")
	sem := Apply(s.Sem, map[string]string{"info": "bright_red", "bogus": "green"})
	if sem.Info.Fg.V16 != 9 {
		t.Errorf("palette 覆盖应叠加于主题之上: info=%d", sem.Info.Fg.V16)
	}
	if s.Sem.Info.Fg.V16 != 6 {
		t.Errorf("Apply 不应修改方案定义: %d", s.Sem.Info.Fg.V16)
	}
}

func TestApplyNilPaletteNoOverride(t *testing.T) {
	s := mustLookup(t, "solar")
	if sem := Apply(s.Sem, nil); sem.Info.Fg.V16 != 6 {
		t.Errorf("无 palette 覆盖时信息色应为 cyan(6): %d", sem.Info.Fg.V16)
	}
}

func TestMinimalThemeUnderlineInlineCode(t *testing.T) {
	s := mustLookup(t, "minimal")
	if s.MD.CodeInline.Attr&rstyle.AttrUnderline == 0 || s.MD.CodeInline.Fg.Kind != rstyle.KindNone {
		t.Errorf("minimal 行内代码应为纯下划线: %+v", s.MD.CodeInline)
	}
	if s.MD.Headings[0].Fg.V16 != 15 || s.MD.Headings[0].Attr&rstyle.AttrBold == 0 {
		t.Errorf("minimal h1 应为亮白粗体: %+v", s.MD.Headings[0])
	}
}

func TestNewThemesDistinct(t *testing.T) {
	nord := mustLookup(t, "nord")
	if nord.Sem.Info.Fg.V16 != 12 || nord.MD.Headings[1].Fg.V16 != 14 {
		t.Errorf("nord 应为亮蓝信息 + 冰青 h2: info=%d h2=%d", nord.Sem.Info.Fg.V16, nord.MD.Headings[1].Fg.V16)
	}
	gruv := mustLookup(t, "gruv")
	if gruv.Sem.Info.Fg.V16 != 3 || gruv.MD.Headings[1].Fg.V16 != 3 {
		t.Errorf("gruv 应为金黄主调: info=%d h2=%d", gruv.Sem.Info.Fg.V16, gruv.MD.Headings[1].Fg.V16)
	}
	dusk := mustLookup(t, "dusk")
	if dusk.Prompt == "" || dusk.MD.Headings[2].Fg.V16 != 13 {
		t.Errorf("dusk 应含紫灰 h3: h3=%d", dusk.MD.Headings[2].Fg.V16)
	}
}
