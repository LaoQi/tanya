package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptBuilderInjectedRead(t *testing.T) {
	ws := "/fake/ws"
	global := "/fake/home/.config/tanya/AGENTS.md"
	files := map[string]string{
		global:                         "全局 G",
		filepath.Join(ws, "AGENTS.md"): "项目 P",
	}
	b := newPromptBuilder(testBasePrompt, ws, global, func(p string) string { return files[p] })
	want := testBasePrompt +
		"\n\n# 全局说明（~/.config/tanya/AGENTS.md）\n\n全局 G" +
		"\n\n# 项目说明（AGENTS.md）\n\n项目 P"
	if b.system() != want {
		t.Errorf("system = %q", b.system())
	}
	if b.legacyPrompt() {
		t.Error("自建提示不应判为旧版")
	}

	files[filepath.Join(ws, "AGENTS.md")] = "项目 P2"
	if b.system() != want {
		t.Error("未 reset 时应保持快照")
	}
	b.reset()
	if !strings.Contains(b.system(), "项目 P2") {
		t.Errorf("reset 应重读文件: %q", b.system())
	}
}

func TestPromptBuilderAdopt(t *testing.T) {
	ws := "/fake/ws"
	global := "/fake/home/.config/tanya/AGENTS.md"
	files := map[string]string{filepath.Join(ws, "AGENTS.md"): "项目 P"}
	b := newPromptBuilder(testBasePrompt, ws, global, func(p string) string { return files[p] })
	b.adopt("文件里的 system")
	if b.system() != "文件里的 system" || b.legacyPrompt() {
		t.Errorf("采纳 system 行失败: %q legacy=%v", b.system(), b.legacyPrompt())
	}
	b.adopt("## 运行环境\n旧版提示")
	if !b.legacyPrompt() {
		t.Error("旧版提示应被识别")
	}
	b.adopt("")
	wantReset := testBasePrompt + "\n\n# 项目说明（AGENTS.md）\n\n项目 P"
	if b.system() != wantReset || b.legacyPrompt() {
		t.Errorf("空 system 应回落重建: %q legacy=%v", b.system(), b.legacyPrompt())
	}
}

func TestPromptBuilderNormalizesBase(t *testing.T) {
	b := newPromptBuilder("线1\r\n线2\n\n", "/fake/ws", "/fake/global", func(string) string { return "" })
	if got := b.system(); got != "线1\n线2" {
		t.Errorf("注入文本应归一化（CRLF→LF、去尾部换行）: %q", got)
	}
	empty := newPromptBuilder("", "/fake/ws", "/fake/global", func(string) string { return "" })
	if got := empty.system(); got != "" {
		t.Errorf("未注入提示词时快照应为空: %q", got)
	}
}

func TestPromptBuilderEmptyBaseSections(t *testing.T) {
	ws := "/fake/ws"
	global := "/fake/home/.config/tanya/AGENTS.md"
	files := map[string]string{
		global:                         "全局 G",
		filepath.Join(ws, "AGENTS.md"): "项目 P",
	}
	read := func(p string) string { return files[p] }
	want := "# 全局说明（~/.config/tanya/AGENTS.md）\n\n全局 G" +
		"\n\n# 项目说明（AGENTS.md）\n\n项目 P"
	b := newPromptBuilder("", ws, global, read)
	if got := b.system(); got != want {
		t.Errorf("空 base 下段拼接不应留前导空行: %q", got)
	}
	onlyProject := map[string]string{filepath.Join(ws, "AGENTS.md"): "项目 P"}
	b2 := newPromptBuilder("", ws, global, func(p string) string { return onlyProject[p] })
	if got, want2 := b2.system(), "# 项目说明（AGENTS.md）\n\n项目 P"; got != want2 {
		t.Errorf("空 base 仅项目段不应留前导空行: %q", got)
	}
}
