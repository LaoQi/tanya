package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BaseURL       string  `yaml:"base_url"`
	APIKey        string  `yaml:"api_key"`
	Model         string  `yaml:"model"`
	Temperature   float64 `yaml:"temperature"`
	SystemPrompt  string  `yaml:"system_prompt"`
	GlobalSession string  `yaml:"global_session"`
	SessionMode   string  `yaml:"session_mode"`
}

func defaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		BaseURL:       "https://api.openai.com/v1",
		Model:         "deepseek-v4-flash",
		Temperature:   0.7,
		GlobalSession: filepath.Join(home, ".local", "share", "tanyan", "sessions"),
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := defaultConfig()
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".config", "tanyan", "config.yaml")
	}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("配置文件解析失败 %s: %w", path, err)
		}
	case !os.IsNotExist(err):
		return nil, err
	}

	if v := os.Getenv("TANYA_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("TANYA_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("TANYA_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("TANYA_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Temperature = f
		}
	}
	if v := os.Getenv("TANYA_SESSION_MODE"); v != "" {
		cfg.SessionMode = v
	}
	cfg.GlobalSession = expandHome(cfg.GlobalSession)
	return cfg, nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
