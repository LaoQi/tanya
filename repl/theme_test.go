package repl

import (
	"strings"
	"testing"
)

func TestThemeCommandNoArg(t *testing.T) {
	r, buf, _ := newTestREPL(t, newFakeTerm())
	r.handleCommand("/theme")
	out := buf.String()
	for _, want := range []string{"当前主题: default", "* default", "minimal", "solar", "vivid"} {
		if !strings.Contains(out, want) {
			t.Errorf("无参输出应包含 %q: %q", want, out)
		}
	}
}

func TestThemeCommandSwitch(t *testing.T) {
	r, buf, _ := newTestREPL(t, newFakeTerm())
	r.handleCommand("/theme vivid")
	out := buf.String()
	if !strings.Contains(out, "主题已切换为 vivid") {
		t.Errorf("切换提示缺失: %q", out)
	}
	want := Semantics("vivid", nil)
	if r.sem.Info.Fg.V16 != 14 || r.sem.Info != want.Info {
		t.Errorf("语义色未切换: info=%d", r.sem.Info.Fg.V16)
	}
	if r.sch.Name != "vivid" {
		t.Errorf("当前方案应为 vivid: %q", r.sch.Name)
	}
	if r.promptTpl != r.sch.Prompt {
		t.Errorf("切换后 promptTpl 应为 vivid 模板: %q", r.promptTpl)
	}
	if got := r.sem.Info.Sprint("x"); got != "\x1b[96mx\x1b[0m" {
		t.Errorf("切换后 Info 应渲染亮青: %q", got)
	}
}

func TestThemeCommandBad(t *testing.T) {
	r, _, errb := newTestREPL(t, newFakeTerm())
	r.handleCommand("/theme bogus")
	out := errb.String()
	if !strings.Contains(out, "无效主题") || !strings.Contains(out, "minimal") {
		t.Errorf("非法主题应报错并列出可用: %q", out)
	}
	if r.sch.Name != "default" {
		t.Errorf("失败切换不应改变主题: %q", r.sch.Name)
	}
}

func TestValidateTheme(t *testing.T) {
	if err := ValidateTheme("nord"); err != nil {
		t.Errorf("内置主题应通过: %v", err)
	}
	if err := ValidateTheme("bogus"); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("非法主题应报错: %v", err)
	}
}

func TestSemanticsPaletteOverlay(t *testing.T) {
	sem := Semantics("nord", map[string]string{"info": "bright_red"})
	if sem.Info.Fg.V16 != 9 {
		t.Errorf("palette 应叠加在方案之上: %d", sem.Info.Fg.V16)
	}
	if Semantics("bogus", nil).Info.Fg.V16 != Semantics("default", nil).Info.Fg.V16 {
		t.Error("非法主题应回落 default")
	}
}
