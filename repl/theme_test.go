package repl

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanyan/style"
)

func resetThemeState(t *testing.T) {
	t.Helper()
	style.ApplyScheme("default")
	style.ApplyPalette(nil)
	t.Cleanup(func() {
		style.ApplyPalette(nil)
		style.ApplyScheme("default")
	})
}

func TestThemeCommandNoArg(t *testing.T) {
	resetThemeState(t)
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
	resetThemeState(t)
	r, buf, _ := newTestREPL(t, newFakeTerm())
	r.handleCommand("/theme vivid")
	out := buf.String()
	if !strings.Contains(out, "主题已切换为 vivid") {
		t.Errorf("切换提示缺失: %q", out)
	}
	sch, ok := style.LookupScheme("vivid")
	if !ok {
		t.Fatal("vivid 应存在")
	}
	if r.promptTpl != sch.Prompt {
		t.Errorf("切换后 promptTpl 应为 vivid 模板: %q", r.promptTpl)
	}
	if style.CurrentSchemeName() != "vivid" || style.Info.Fg.V16 != 14 {
		t.Errorf("全局语义色未切换: scheme=%q info=%d", style.CurrentSchemeName(), style.Info.Fg.V16)
	}
	if got := style.Info.Sprint("x"); got != "\x1b[96mx\x1b[0m" {
		t.Errorf("切换后 Info 应渲染亮青: %q", got)
	}
}

func TestThemeCommandBad(t *testing.T) {
	resetThemeState(t)
	r, _, errb := newTestREPL(t, newFakeTerm())
	r.handleCommand("/theme bogus")
	out := errb.String()
	if !strings.Contains(out, "无效主题") || !strings.Contains(out, "minimal") {
		t.Errorf("非法主题应报错并列出可用: %q", out)
	}
	if style.CurrentSchemeName() != "default" {
		t.Errorf("失败切换不应改变主题: %q", style.CurrentSchemeName())
	}
}
