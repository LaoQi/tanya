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
	b := newPromptBuilder(testBasePrompt, ws, global, "/fake/home", func(p string) string { return files[p] })
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
	b := newPromptBuilder(testBasePrompt, ws, global, "/fake/home", func(p string) string { return files[p] })
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
	b := newPromptBuilder("线1\r\n线2\n\n", "/fake/ws", "/fake/global", "", func(string) string { return "" })
	if got := b.system(); got != "线1\n线2" {
		t.Errorf("注入文本应归一化（CRLF→LF、去尾部换行）: %q", got)
	}
	empty := newPromptBuilder("", "/fake/ws", "/fake/global", "", func(string) string { return "" })
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
	b := newPromptBuilder("", ws, global, "/fake/home", read)
	if got := b.system(); got != want {
		t.Errorf("空 base 下段拼接不应留前导空行: %q", got)
	}
	onlyProject := map[string]string{filepath.Join(ws, "AGENTS.md"): "项目 P"}
	b2 := newPromptBuilder("", ws, global, "/fake/home", func(p string) string { return onlyProject[p] })
	if got, want2 := b2.system(), "# 项目说明（AGENTS.md）\n\n项目 P"; got != want2 {
		t.Errorf("空 base 仅项目段不应留前导空行: %q", got)
	}
}

func TestPromptBuilderCustomGlobalPath(t *testing.T) {
	ws := "/fake/ws"
	home := "/home/u"
	global := filepath.Join(home, ".config", "ivan", "AGENTS.md")
	files := map[string]string{global: "ivan 全局"}
	b := newPromptBuilder("B", ws, global, home, func(p string) string { return files[p] })
	if !strings.Contains(b.system(), "# 全局说明（~/.config/ivan/AGENTS.md）") {
		t.Errorf("标题应显示实际路径: %q", b.system())
	}
	if strings.Contains(b.system(), "tanya") {
		t.Errorf("不应再出现 tanya 路径: %q", b.system())
	}

	outside := "/opt/elsewhere/AGENTS.md"
	files[outside] = "外部全局"
	b2 := newPromptBuilder("B", ws, outside, home, func(p string) string { return files[p] })
	if !strings.Contains(b2.system(), "# 全局说明（/opt/elsewhere/AGENTS.md）") {
		t.Errorf("home 之外的路径应原样显示: %q", b2.system())
	}

	b3 := newPromptBuilder("B", ws, "", home, func(p string) string { return files[outside] })
	if strings.Contains(b3.system(), "全局说明") {
		t.Errorf("空路径不应读全局说明: %q", b3.system())
	}
}
