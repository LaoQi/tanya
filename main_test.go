package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/LaoQi/tanya/config"
	"github.com/LaoQi/tanya/ctty"
	"github.com/LaoQi/tanya/repl"
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
	var got config.Config
	if err := yaml.Unmarshal([]byte(configExampleFile), &got); err != nil {
		t.Fatalf("示例应可被 yaml 解析: %v", err)
	}
	want := config.Default()
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

func TestUsageStartsWithHead(t *testing.T) {
	fs := flag.NewFlagSet("tanya", flag.ContinueOnError)
	registerFlags(fs)
	var buf bytes.Buffer
	writeUsage(&buf, fs)
	if got := buf.String(); !strings.HasPrefix(got, repl.UsageHead) {
		t.Errorf("用法应以 UsageHead 开头:\n%s", got)
	}
}

func TestUsageListsAllFlags(t *testing.T) {
	fs := flag.NewFlagSet("tanya", flag.ContinueOnError)
	registerFlags(fs)
	var buf bytes.Buffer
	writeUsage(&buf, fs)
	got := buf.String()
	fs.VisitAll(func(f *flag.Flag) {
		if !strings.Contains(got, "-"+f.Name) {
			t.Errorf("用法应列出选项 -%s:\n%s", f.Name, got)
		}
	})
}

func TestRootRefusalMatrix(t *testing.T) {
	refusedByDefault := ctty.IsRoot()
	cases := []struct {
		env     string
		refused bool
	}{
		{"", refusedByDefault},
		{"1", false},
		{"true", refusedByDefault},
		{"0", refusedByDefault},
	}
	for _, c := range cases {
		t.Setenv(envAllowRoot, c.env)
		err := rootRefusal()
		if (err != nil) != c.refused {
			t.Errorf("%s=%q: err = %v, want refused=%v", envAllowRoot, c.env, err, c.refused)
			continue
		}
		if err != nil && err.Error() != repl.MsgRootRefused {
			t.Errorf("%s=%q: 文案 = %q, want %q", envAllowRoot, c.env, err.Error(), repl.MsgRootRefused)
		}
	}
}
