package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/tools/shell"
)

func stubLookPath(name string) (string, error) { return "/usr/bin/" + name, nil }

func toolNames(list []agent.Tool) []string {
	out := make([]string, len(list))
	for i, tool := range list {
		out[i] = tool.Name()
	}
	return out
}

func TestStandardOrderAndDefs(t *testing.T) {
	list, err := Standard(Options{LookPath: stubLookPath})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run_shell", "get_time", "get_env", "calc"}
	got := toolNames(list)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("标准工具集顺序应为 %v: %v", want, got)
	}
	for _, tool := range list {
		def := tool.Definition()
		if def.Type != "function" || def.Function.Name != tool.Name() {
			t.Errorf("%s definition 头异常: %+v", tool.Name(), def)
		}
		if def.Function.Description == "" || len(def.Function.Parameters) == 0 {
			t.Errorf("%s definition 不完整: %+v", tool.Name(), def)
		}
	}
}

func TestShellResolvesOverride(t *testing.T) {
	notFound := func(string) (string, error) { return "", errors.New("not found") }
	if _, err := Shell(Options{ShellOverride: "/no/such/shell-tanya", LookPath: notFound}); err == nil {
		t.Error("不可用的 shell 覆盖应报错")
	}
	tool, err := Shell(Options{ShellOverride: "zsh", LookPath: stubLookPath})
	if err != nil {
		t.Fatal(err)
	}
	if tool.Name() != "run_shell" {
		t.Errorf("工具名: %q", tool.Name())
	}
}

func testAgentConfig(t *testing.T) *agent.Config {
	t.Helper()
	dir := t.TempDir()
	return &agent.Config{
		BaseURL:          "http://127.0.0.1:1/v1",
		APIKey:           "test-key",
		Model:            "test-model",
		ApiProtocol:      "responses",
		UserAgent:        agent.DefaultUserAgent,
		DataDir:          dir,
		SessionMode:      "global",
		AutoArchive:      true,
		ArchiveThreshold: agent.DefaultArchiveThreshold,
		ArchiveKeep:      agent.DefaultArchiveKeep,
		ConfigPath:       filepath.Join(dir, "config.yaml"),
	}
}

func evalDir(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestStandardToolsFollowWorkspace(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("当前环境无 bash: %v", err)
	}
	home, _ := os.UserHomeDir()
	var ag *agent.Agent
	list, err := Standard(Options{
		Home:      home,
		Workspace: func() string { return ag.Workspace() },
		LookPath:  exec.LookPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	ag, err = agent.New(testAgentConfig(t), agent.WithTools(list...))
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := ag.SwitchWorkspace(target); err != nil {
		t.Fatal(err)
	}
	res := list[0].Invoke(context.Background(), `{"command":"pwd"}`)
	sh, ok := res.Meta.(*shell.Result)
	if !ok || sh == nil {
		t.Fatalf("run_shell 应携带结构化结果: %+v", res)
	}
	if sh.Cwd != "" {
		t.Errorf("默认目录不应回显 cwd: %q", sh.Cwd)
	}
	var out strings.Builder
	for _, c := range sh.Stdout {
		out.WriteString(c.Data)
	}
	if got, want := evalDir(t, strings.TrimSpace(out.String())), evalDir(t, target); got != want {
		t.Errorf("run_shell 默认目录应跟随 /switch 后的工作区: got %q want %q", got, want)
	}
}
