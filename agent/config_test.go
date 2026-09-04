package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.BaseURL == "" || cfg.Model == "" {
		t.Error("默认值不应为空")
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("默认数值异常: %+v", cfg)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != defaultConfig().Model {
		t.Error("缺文件时应使用默认值")
	}
}

func TestLoadConfigYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `
model: yaml-model
temperature: 0.3
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "yaml-model" || cfg.Temperature != 0.3 {
		t.Errorf("yaml 覆盖失败: %+v", cfg)
	}
}

func TestLoadConfigEnvOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("model: yaml-model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANYA_MODEL", "env-model")
	t.Setenv("TANYA_API_KEY", "env-key")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "env-model" {
		t.Errorf("env 应覆盖 yaml: %q", cfg.Model)
	}
	if cfg.APIKey != "env-key" {
		t.Errorf("TANYA_API_KEY 未生效")
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("model: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Error("非法 yaml 应报错")
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := expandHome("~/x/y"); got != filepath.Join(home, "x", "y") {
		t.Errorf("got %q", got)
	}
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("got %q", got)
	}
	if strings.HasPrefix(expandHome("~"), home) && len(expandHome("~")) == len(home) {
		t.Error("单独的 ~ 不在展开范围")
	}
}
