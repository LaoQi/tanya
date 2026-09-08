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
	BaseURL         string  `yaml:"base_url"`
	APIKey          string  `yaml:"api_key"`
	Model           string  `yaml:"model"`
	Temperature     float64 `yaml:"temperature"`
	ReasoningEffort string  `yaml:"reasoning_effort"`
	ApiProtocol     string  `yaml:"api_protocol"`
	UserAgent       string  `yaml:"user_agent"`
	GlobalSession   string  `yaml:"global_session"`
	SessionMode     string  `yaml:"session_mode"`
	ToolOutputLines int     `yaml:"tool_output_lines"`
	Shell           string  `yaml:"shell"`
}

var EffortLevels = []string{"minimal", "low", "medium", "high", "max"}

var ApiProtocols = []string{"chat", "responses"}

func normalizeEffort(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, e := range EffortLevels {
		if v == e {
			return e
		}
	}
	return ""
}

func normalizeApiProtocol(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, p := range ApiProtocols {
		if v == p {
			return v
		}
	}
	return ""
}

const DefaultPrompt = "\x1b[37m{cwd}\x1b[0m \x1b[34m{model}\x1b[0m \x1b[33m{effort}\x1b[0m \x1b[32m{stat}\x1b[0m \x1b[37m>\x1b[0m "

const DefaultUserAgent = "pi/0.85.0 (linux; node/v22.14.0; x64)"

func defaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		BaseURL:         "https://api.openai.com/v1",
		Model:           "deepseek-v4-flash",
		Temperature:     0.7,
		ApiProtocol:     "responses",
		UserAgent:       DefaultUserAgent,
		GlobalSession:   filepath.Join(home, ".local", "share", "tanyan", "sessions"),
		ToolOutputLines: 20,
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
			return nil, fmt.Errorf(MsgConfigParse, path, err)
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
	if v := os.Getenv("TANYA_REASONING_EFFORT"); v != "" {
		cfg.ReasoningEffort = v
	}
	if v := os.Getenv("TANYA_API_PROTOCOL"); v != "" {
		cfg.ApiProtocol = v
	}
	if v := os.Getenv("TANYA_SESSION_MODE"); v != "" {
		cfg.SessionMode = v
	}
	if v := os.Getenv("TANYA_USER_AGENT"); v != "" {
		cfg.UserAgent = v
	}
	if v := os.Getenv("TANYA_SHELL"); v != "" {
		cfg.Shell = v
	}
	if v := os.Getenv("TANYA_TOOL_OUTPUT_LINES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ToolOutputLines = n
		}
	}
	if cfg.ToolOutputLines < 1 || cfg.ToolOutputLines > 1000 {
		cfg.ToolOutputLines = 20
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = DefaultUserAgent
	}
	cfg.ReasoningEffort = normalizeEffort(cfg.ReasoningEffort)
	rawProtocol := cfg.ApiProtocol
	cfg.ApiProtocol = normalizeApiProtocol(cfg.ApiProtocol)
	if cfg.ApiProtocol == "" {
		return nil, fmt.Errorf(MsgBadApiProtocol, rawProtocol)
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
