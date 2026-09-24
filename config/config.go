package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/LaoQi/tanya/agent"
)

type UI struct {
	ShowReasoning   bool              `yaml:"show_reasoning"`
	Bell            bool              `yaml:"bell"`
	NotifyOSC       bool              `yaml:"notify_osc"`
	NotifyCmd       string            `yaml:"notify_cmd"`
	Colors          string            `yaml:"colors"`
	Theme           string            `yaml:"theme"`
	Palette         map[string]string `yaml:"palette"`
	ToolOutputLines int               `yaml:"tool_output_lines"`
}

type Config struct {
	agent.Config `yaml:",inline"`
	UI           `yaml:",inline"`
	Path         string `yaml:"-"`
}

const (
	DefaultToolOutputLines  = 20
	DefaultTheme            = "nord"
	DefaultSessionMode      = "auto"
	DefaultDataDirSuffix    = ".local/share/tanya"
	DefaultConfigPathSuffix = ".config/tanya/config.yaml"
)

func defaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, filepath.FromSlash(DefaultDataDirSuffix))
}

func defaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, filepath.FromSlash(DefaultConfigPathSuffix))
}

func Default() *Config {
	return &Config{
		Config: agent.Config{
			BaseURL:          "https://api.openai.com/v1",
			Model:            "deepseek-v4-flash",
			Temperature:      0.7,
			ApiProtocol:      "responses",
			SessionMode:      DefaultSessionMode,
			UserAgent:        agent.DefaultUserAgent,
			DataDir:          defaultDataDir(),
			AutoArchive:      true,
			ArchiveThreshold: agent.DefaultArchiveThreshold,
			ArchiveKeep:      agent.DefaultArchiveKeep,
		},
		Path: defaultConfigPath(),
		UI: UI{
			Theme:           DefaultTheme,
			ToolOutputLines: DefaultToolOutputLines,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = cfg.Path
	} else {
		path = normalizeConfigPath(path)
	}
	cfg.Path = path
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf(agent.MsgConfigParse, path, err)
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
	if v := os.Getenv("TANYA_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("TANYA_SESSION_MODE"); v != "" {
		cfg.SessionMode = v
	}
	if v := os.Getenv("TANYA_THEME"); v != "" {
		cfg.Theme = v
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
		cfg.ToolOutputLines = DefaultToolOutputLines
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = agent.DefaultUserAgent
	}
	cfg.NotifyCmd = strings.TrimSpace(cfg.NotifyCmd)
	cfg.ReasoningEffort = agent.NormalizeEffort(cfg.ReasoningEffort)
	rawProtocol := cfg.ApiProtocol
	cfg.ApiProtocol = agent.NormalizeApiProtocol(cfg.ApiProtocol)
	if cfg.ApiProtocol == "" {
		return nil, fmt.Errorf(agent.MsgBadApiProtocol, rawProtocol)
	}
	cfg.SessionMode = strings.ToLower(strings.TrimSpace(cfg.SessionMode))
	cfg.DataDir = expandHome(cfg.DataDir)
	if cfg.DataDir == "" {
		cfg.DataDir = defaultDataDir()
	}
	if cfg.SessionMode == "" {
		cfg.SessionMode = DefaultSessionMode
	}
	cfg.ConfigPath = cfg.Path

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf(agent.MsgNilConfig)
	}
	return c.Config.Validate()
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func normalizeConfigPath(p string) string {
	p = expandHome(p)
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
