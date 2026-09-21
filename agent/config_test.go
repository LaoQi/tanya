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

func TestConfigPathDefaultAndExplicit(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Path == "" || !filepath.IsAbs(cfg.Path) {
		t.Fatalf("默认配置路径应非空且绝对: %q", cfg.Path)
	}
	path := filepath.Join(t.TempDir(), "nonexistent.yaml")
	fallback, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Path != path {
		t.Errorf("显式路径应记录生效值: %q", fallback.Path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeConfigPath("~/x.yaml"); got != filepath.Join(home, "x.yaml") {
		t.Errorf("波浪号展开: %q", got)
	}
	if got := normalizeConfigPath("rel.yaml"); !filepath.IsAbs(got) {
		t.Errorf("相对路径应绝对化: %q", got)
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
	c := NewClient(cfg, nil)
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

func TestConfigTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("theme: solar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "solar" {
		t.Errorf("yaml theme 未生效: %q", cfg.Theme)
	}
	t.Setenv("TANYA_THEME", "minimal")
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "minimal" {
		t.Errorf("env 应覆盖 yaml: %q", cfg.Theme)
	}
	t.Setenv("TANYA_THEME", "")
	if cfg := defaultConfig(); cfg.Theme != "nord" {
		t.Errorf("默认主题应为 nord: %q", cfg.Theme)
	}
}

func TestLoadConfigShowReasoning(t *testing.T) {
	if defaultConfig().ShowReasoning {
		t.Error("默认应为关闭")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("show_reasoning: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ShowReasoning {
		t.Error("yaml show_reasoning 未生效")
	}
	if err := os.WriteFile(path, []byte("show_reasoning: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, err = LoadConfig(path); err != nil || cfg.ShowReasoning {
		t.Errorf("显式关闭应生效: %v %v", cfg.ShowReasoning, err)
	}
}

func TestLoadConfigAutoArchive(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantAuto bool
		wantErr  bool
	}{
		{name: "默认开启", content: "model: m", wantAuto: true},
		{name: "显式关闭", content: "auto_archive: false\n"},
		{name: "阈值过小", content: "auto_archive_threshold: 1\n", wantErr: true},
		{name: "阈值过小且保留数合法", content: "auto_archive_threshold: 1\nauto_archive_keep: 0\n", wantErr: true},
		{name: "保留数为负", content: "auto_archive_keep: -1\n", wantErr: true},
		{name: "保留数不小于阈值", content: "auto_archive_threshold: 4\nauto_archive_keep: 4\n", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if c.wantErr {
				if err == nil {
					t.Fatalf("应报错: %+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ArchiveThreshold != DefaultArchiveThreshold || cfg.ArchiveKeep != DefaultArchiveKeep {
				t.Errorf("默认阈值/保留数异常: %+v", cfg)
			}
			if cfg.AutoArchive != c.wantAuto {
				t.Errorf("开关默认值异常: AutoArchive=%v want %v", cfg.AutoArchive, c.wantAuto)
			}
		})
	}
}

func TestLoadConfigAutoArchiveValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("auto_archive: true\nauto_archive_threshold: 8\nauto_archive_keep: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoArchive || cfg.ArchiveThreshold != 8 || cfg.ArchiveKeep != 3 {
		t.Errorf("yaml 覆盖失败: %+v", cfg)
	}
}

func TestLoadConfigNotify(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "notify_osc: true\nnotify_cmd: \"  notify-send -a tanya {title} {content}  \"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NotifyOSC {
		t.Error("notify_osc 未生效")
	}
	if cfg.NotifyCmd != "notify-send -a tanya {title} {content}" {
		t.Errorf("notify_cmd 应去空白: %q", cfg.NotifyCmd)
	}
	if cfg := defaultConfig(); cfg.NotifyOSC || cfg.NotifyCmd != "" {
		t.Errorf("通知默认应为关闭: osc=%v cmd=%q", cfg.NotifyOSC, cfg.NotifyCmd)
	}
}
