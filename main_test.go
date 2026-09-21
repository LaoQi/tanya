package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/LaoQi/tanya/agent"
)

func TestConfigExampleEmbedded(t *testing.T) {
	if strings.TrimSpace(configExampleFile) == "" {
		t.Fatal("嵌入的 config.example.yaml 为空")
	}
	if !strings.HasSuffix(configExampleFile, "\n") {
		t.Error("嵌入文本应以换行结尾")
	}
	if strings.Contains(configExampleFile, "\r") {
		t.Error("嵌入文本不应含 CR")
	}
}

func TestConfigExampleMatchesDefaults(t *testing.T) {
	var got agent.Config
	if err := yaml.Unmarshal([]byte(configExampleFile), &got); err != nil {
		t.Fatalf("示例应可被 yaml 解析: %v", err)
	}
	want := agent.DefaultConfig()
	checks := []struct {
		key  string
		got  string
		want string
	}{
		{"base_url", got.BaseURL, want.BaseURL},
		{"api_key", got.APIKey, want.APIKey},
		{"model", got.Model, want.Model},
		{"temperature", strconv.FormatFloat(got.Temperature, 'g', -1, 64), strconv.FormatFloat(want.Temperature, 'g', -1, 64)},
		{"user_agent", got.UserAgent, want.UserAgent},
		{"data_dir", expandTilde(got.DataDir), want.DataDir},
		{"session_mode", got.SessionMode, want.SessionMode},
		{"tool_output_lines", strconv.Itoa(got.ToolOutputLines), strconv.Itoa(want.ToolOutputLines)},
	}
	covered := make(map[string]bool, len(checks))
	for _, c := range checks {
		covered[c.key] = true
		if c.got != c.want {
			t.Errorf("示例的 %s = %q，代码默认值 = %q", c.key, c.got, c.want)
		}
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(configExampleFile), &node); err != nil {
		t.Fatalf("示例应可被 yaml 解析: %v", err)
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		t.Fatal("示例应为顶层 yaml 映射")
	}
	root := node.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if key := root.Content[i].Value; !covered[key] {
			t.Errorf("示例显式键 %q 未纳入默认值比对", key)
		}
	}
}

func expandTilde(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, p[2:])
}
