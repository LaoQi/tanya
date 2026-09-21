package agent

import (
	"os"
	"path/filepath"
	"strings"
)

type promptBuilder struct {
	base       string
	cwd        string
	globalPath string
	read       func(string) string
	snapshot   string
	legacy     bool
}

func newPromptBuilder(base, cwd, globalPath string, read func(string) string) *promptBuilder {
	p := &promptBuilder{base: normalizePrompt(base), cwd: cwd, globalPath: globalPath, read: read}
	p.reset()
	return p
}

func normalizePrompt(s string) string {
	return strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func (p *promptBuilder) reset() {
	p.snapshot = p.build()
	p.legacy = false
}

func (p *promptBuilder) build() string {
	prompt := p.base
	if global := p.read(p.globalPath); global != "" {
		if prompt != "" {
			prompt += "\n\n"
		}
		prompt += "# 全局说明（~/.config/tanya/AGENTS.md）\n\n" + global
	}
	if project := p.read(filepath.Join(p.cwd, "AGENTS.md")); project != "" {
		if prompt != "" {
			prompt += "\n\n"
		}
		prompt += "# 项目说明（AGENTS.md）\n\n" + project
	}
	return prompt
}

func (p *promptBuilder) adopt(system string) {
	if system == "" {
		p.reset()
		return
	}
	p.snapshot = system
	p.legacy = isLegacyPrompt(system)
}

func (p *promptBuilder) system() string { return p.snapshot }

func (p *promptBuilder) legacyPrompt() bool { return p.legacy }

func (p *promptBuilder) runtime(env string) string {
	if p.snapshot == "" {
		return env
	}
	if env == "" {
		return p.snapshot
	}
	return p.snapshot + "\n\n" + env
}

func globalAgentsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tanya", "AGENTS.md")
}

func readAgentsFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func isLegacyPrompt(p string) bool {
	return strings.Contains(p, "## 运行环境") || strings.Contains(p, "## 可用工具")
}
