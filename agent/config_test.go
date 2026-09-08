package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	if cfg.ApiProtocol != "responses" {
		t.Errorf("默认协议应为 responses: %q", cfg.ApiProtocol)
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("默认数值异常: %+v", cfg)
	}
	if !strings.Contains(DefaultPrompt, "{cwd}") || !strings.Contains(DefaultPrompt, "{stat}") {
		t.Errorf("默认 prompt 模板异常: %q", DefaultPrompt)
	}
	if cfg.UserAgent != DefaultUserAgent || !strings.HasPrefix(cfg.UserAgent, "pi/") {
		t.Errorf("默认 UA 异常: %q", cfg.UserAgent)
	}
}

func TestLoadConfigApiProtocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("api_protocol: chat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApiProtocol != "chat" {
		t.Errorf("yaml api_protocol 未生效: %q", cfg.ApiProtocol)
	}
	t.Setenv("TANYA_API_PROTOCOL", "RESPONSES")
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApiProtocol != "responses" {
		t.Errorf("env 应覆盖 yaml 并归一小写: %q", cfg.ApiProtocol)
	}
	t.Setenv("TANYA_API_PROTOCOL", "")
	if err := os.WriteFile(path, []byte("api_protocol: bogus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("非法 api_protocol 应报错: %v", err)
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

func TestLoadConfigReasoningEffort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("reasoning_effort: high\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReasoningEffort != "high" {
		t.Errorf("yaml reasoning_effort 未生效: %q", cfg.ReasoningEffort)
	}
	t.Setenv("TANYA_REASONING_EFFORT", "MAX")
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReasoningEffort != "max" {
		t.Errorf("env 应覆盖 yaml 并归一小写: %q", cfg.ReasoningEffort)
	}
	t.Setenv("TANYA_REASONING_EFFORT", "")
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReasoningEffort != "high" {
		t.Errorf("空 env 不应覆盖: %q", cfg.ReasoningEffort)
	}
	if err := os.WriteFile(path, []byte("reasoning_effort: bogus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReasoningEffort != "" {
		t.Errorf("非法值应置空: %q", cfg.ReasoningEffort)
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

func TestLoadConfigShell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("shell: zsh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Shell != "zsh" {
		t.Errorf("yaml shell 覆盖失败: %q", cfg.Shell)
	}
	t.Setenv("TANYA_SHELL", "/usr/bin/fish")
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Shell != "/usr/bin/fish" {
		t.Errorf("env 应覆盖 yaml: %q", cfg.Shell)
	}
	if cfg := defaultConfig(); cfg.Shell != "" {
		t.Errorf("默认 shell 应为空（自动探测）: %q", cfg.Shell)
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

func TestLoadConfigPromptIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("prompt: \"\\x1b[35m{model} >\\x1b[0m \"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANYA_PROMPT", "[{cwd}] ")
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(DefaultPrompt, "{stat}") {
		t.Errorf("默认模板应为富版: %q", DefaultPrompt)
	}
}

func TestUserAgentHeader(t *testing.T) {
	var ua, modelUA string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		modelUA = r.Header.Get("User-Agent")
		fmt.Fprint(w, `{"data":[{"id":"m1"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL + "/v1"
	cfg.APIKey = "test-key"
	cfg.ApiProtocol = "chat"
	c := NewClient(cfg)
	if _, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListModels(); err != nil {
		t.Fatal(err)
	}
	if ua != DefaultUserAgent || modelUA != DefaultUserAgent {
		t.Errorf("UA 头异常: chat=%q models=%q", ua, modelUA)
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

func TestToolOutputLines(t *testing.T) {
	if cfg := defaultConfig(); cfg.ToolOutputLines != 20 {
		t.Errorf("默认应为 20: %d", cfg.ToolOutputLines)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("tool_output_lines: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolOutputLines != 5 {
		t.Errorf("yaml 覆盖失败: %d", cfg.ToolOutputLines)
	}
	if err := os.WriteFile(path, []byte("tool_output_lines: -3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolOutputLines != 20 {
		t.Errorf("非法值应回退 20: %d", cfg.ToolOutputLines)
	}
	t.Setenv("TANYA_TOOL_OUTPUT_LINES", "7")
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolOutputLines != 7 {
		t.Errorf("env 覆盖失败: %d", cfg.ToolOutputLines)
	}
}

func TestLoadConfigStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "colors: off\npalette:\n  info: bright_red\n  error: green\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Colors != "off" {
		t.Errorf("colors 解析失败: %q", cfg.Colors)
	}
	if cfg.Palette["info"] != "bright_red" || cfg.Palette["error"] != "green" {
		t.Errorf("palette 解析失败: %v", cfg.Palette)
	}
}
