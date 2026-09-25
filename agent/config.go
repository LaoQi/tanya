package agent

import (
	"errors"
	"fmt"
	"strings"
)

type Config struct {
	BaseURL          string  `yaml:"base_url"`
	APIKey           string  `yaml:"api_key"`
	Model            string  `yaml:"model"`
	Temperature      float64 `yaml:"temperature"`
	ReasoningEffort  string  `yaml:"reasoning_effort"`
	ApiProtocol      string  `yaml:"api_protocol"`
	UserAgent        string  `yaml:"user_agent"`
	DataDir          string  `yaml:"data_dir"`
	SessionMode      string  `yaml:"session_mode"`
	AutoArchive      bool    `yaml:"auto_archive"`
	ArchiveThreshold int     `yaml:"auto_archive_threshold"`
	ArchiveKeep      int     `yaml:"auto_archive_keep"`
	ConfigPath       string  `yaml:"-"`
}

var EffortLevels = []string{"minimal", "low", "medium", "high", "max"}

var ApiProtocols = []string{"chat", "responses"}

var SessionModes = []string{"auto", "local", "global"}

const DefaultUserAgent = "pi/0.85.0 (linux; node/v22.14.0; x64)"

func NormalizeEffort(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, e := range EffortLevels {
		if v == e {
			return v
		}
	}
	return ""
}

func NormalizeApiProtocol(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, p := range ApiProtocols {
		if v == p {
			return v
		}
	}
	return ""
}

func validSessionMode(v string) bool {
	for _, m := range SessionModes {
		if v == m {
			return true
		}
	}
	return false
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New(MsgNilConfig)
	}
	switch {
	case c.BaseURL == "":
		return errors.New(MsgEmptyBaseURL)
	case c.Model == "":
		return errors.New(MsgEmptyModel)
	case c.UserAgent == "":
		return errors.New(MsgEmptyUserAgent)
	case c.DataDir == "":
		return errors.New(MsgEmptyDataDir)
	case c.ConfigPath == "":
		return errors.New(MsgEmptyConfigPath)
	}
	if NormalizeApiProtocol(c.ApiProtocol) == "" {
		return fmt.Errorf(MsgBadApiProtocol, c.ApiProtocol)
	}
	if !validSessionMode(c.SessionMode) {
		return fmt.Errorf(MsgBadSessionMode, c.SessionMode)
	}
	if c.ArchiveThreshold < 2 {
		return fmt.Errorf(MsgBadArchiveThreshold, c.ArchiveThreshold)
	}
	if c.ArchiveKeep < 0 || c.ArchiveKeep >= c.ArchiveThreshold {
		return fmt.Errorf(MsgBadArchiveKeep, c.ArchiveKeep, c.ArchiveThreshold)
	}
	return nil
}
