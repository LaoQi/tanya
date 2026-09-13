package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptBuilderInjectedRead(t *testing.T) {
	ws := "/fake/ws"
	global := "/fake/home/.config/tanyan/AGENTS.md"
	files := map[string]string{
		global:                         "全局 G",
		filepath.Join(ws, "AGENTS.md"): "项目 P",
	}
	b := newPromptBuilder(ws, global, func(p string) string { return files[p] })
	want := DefaultSystemPrompt +
		"\n\n# 全局说明（~/.config/tanyan/AGENTS.md）\n\n全局 G" +
		"\n\n# 项目说明（AGENTS.md）\n\n项目 P"
	if b.system() != want {
		t.Errorf("system = %q", b.system())
	}
	if b.legacyPrompt() {
		t.Error("自建提示不应判为旧版")
	}
	if b.runtime("") != want {
		t.Errorf("空 env 不应追加分隔符: %q", b.runtime(""))
	}
	if got := b.runtime("# 环境"); got != want+"\n\n# 环境" {
		t.Errorf("env 应追加在快照之后: %q", got)
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
	global := "/fake/home/.config/tanyan/AGENTS.md"
	files := map[string]string{filepath.Join(ws, "AGENTS.md"): "项目 P"}
	b := newPromptBuilder(ws, global, func(p string) string { return files[p] })
	b.adopt("文件里的 system")
	if b.system() != "文件里的 system" || b.legacyPrompt() {
		t.Errorf("采纳 system 行失败: %q legacy=%v", b.system(), b.legacyPrompt())
	}
	b.adopt("## 运行环境\n旧版提示")
	if !b.legacyPrompt() {
		t.Error("旧版提示应被识别")
	}
	b.adopt("")
	wantReset := DefaultSystemPrompt + "\n\n# 项目说明（AGENTS.md）\n\n项目 P"
	if b.system() != wantReset || b.legacyPrompt() {
		t.Errorf("空 system 应回落重建: %q legacy=%v", b.system(), b.legacyPrompt())
	}
}
