package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func testShellTool(t *testing.T, mutate ...func(*shellToolConfig)) *shellTool {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	cfg := shellToolConfig{GOOS: runtime.GOOS, LookPath: exec.LookPath, Home: home, Workspace: cwd}
	for _, f := range mutate {
		f(&cfg)
	}
	tool, err := newShellTool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func testToolDefs() []ToolDef {
	return ToolDefs(&shellTool{profile: &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix}})
}

func runShellString(t *testing.T, ctx context.Context, command string, timeoutSec int) string {
	t.Helper()
	return testShellTool(t).run(ctx, shellRequest{Command: command, TimeoutSec: timeoutSec}).String()
}

func TestNewShellToolResolvesProfileAndPrograms(t *testing.T) {
	tool, err := newShellTool(shellToolConfig{GOOS: "linux", LookPath: lookPathStub("bash", "ls")})
	if err != nil || tool.profile == nil || tool.profile.Name != "bash" {
		t.Fatalf("newShellTool: %+v err=%v", tool, err)
	}
	if strings.Join(tool.programs, ",") != "ls" {
		t.Errorf("programs = %v", tool.programs)
	}
	if tool.profile.invocation() != "bash -c" {
		t.Errorf("invocation = %q", tool.profile.invocation())
	}
}

func TestNewShellToolInjectedPrograms(t *testing.T) {
	tool, err := newShellTool(shellToolConfig{
		GOOS:     "linux",
		LookPath: lookPathStub("bash", "ls", "grep"),
		Programs: []string{"ls"},
	})
	if err != nil || strings.Join(tool.programs, ",") != "ls" {
		t.Fatalf("Programs 注入应跳过探针: %+v err=%v", tool, err)
	}
}

func TestNewShellToolNoShell(t *testing.T) {
	if tool, err := newShellTool(shellToolConfig{GOOS: "linux", LookPath: lookPathStub()}); err == nil || tool != nil {
		t.Errorf("无 shell 应报错: %+v err=%v", tool, err)
	}
}

func TestNewShellToolOverrideUnavailable(t *testing.T) {
	_, err := newShellTool(shellToolConfig{Override: "zsh", GOOS: "linux", LookPath: lookPathStub("bash")})
	if err == nil || !strings.Contains(err.Error(), "配置的 shell") {
		t.Fatalf("override 无效应报错: %v", err)
	}
}

func TestShellToolResolveCwdInjected(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(ws, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(ws, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := testShellTool(t, func(c *shellToolConfig) { c.Home = home; c.Workspace = ws })
	cases := []struct{ in, want string }{
		{"", ""},
		{home, home},
		{"~", home},
		{"~/", home},
		{"~/x", filepath.Join(home, "x")},
		{".", ws},
		{"sub", sub},
		{filepath.Join(sub, ".."), ws},
	}
	for _, c := range cases {
		got, err := tool.resolveCwd(c.in)
		if err != nil || got != c.want {
			t.Errorf("resolveCwd(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{filepath.Join(ws, "nope"), file, "~user", "/no/such/dir-tanyan"} {
		if got, err := tool.resolveCwd(bad); err == nil {
			t.Errorf("resolveCwd(%q) 应报错，得到 %q", bad, got)
		}
	}
}

func TestShellToolResolveCwdNoBaseline(t *testing.T) {
	tool := testShellTool(t, func(c *shellToolConfig) { c.Home = ""; c.Workspace = "" })
	if got, err := tool.resolveCwd("sub"); err == nil || got != "" {
		t.Errorf("无工作区基准时相对路径应报错: got %q, %v", got, err)
	}
	if got, err := tool.resolveCwd("~"); err == nil || got != "" {
		t.Errorf("无家目录时 ~ 应报错: got %q, %v", got, err)
	}
	if got, err := tool.resolveCwd("/tmp"); err != nil || got != "/tmp" {
		t.Errorf("绝对路径不依赖注入值: got %q, %v", got, err)
	}
}

func TestShellToolToolDesc(t *testing.T) {
	tool := testShellTool(t, func(c *shellToolConfig) { c.Programs = []string{"ls", "grep"} })
	desc := tool.toolDesc()
	if !strings.Contains(desc, "可用程序: ls, grep") || !strings.Contains(desc, "cwd 参数") {
		t.Errorf("toolDesc = %q", desc)
	}
}

func TestShellToolRunNonInteractive(t *testing.T) {
	res := testShellTool(t).run(context.Background(), shellRequest{Command: "echo hi", TimeoutSec: 10})
	if res.ExitCode != 0 || res.Err != "" || !strings.Contains(res.String(), "hi") {
		t.Fatalf("run: %+v", res)
	}
}

func TestShellToolRunBadCwd(t *testing.T) {
	res := testShellTool(t).run(context.Background(), shellRequest{
		Command:    "echo hi",
		TimeoutSec: 10,
		Cwd:        filepath.Join(t.TempDir(), "nope"),
	})
	if !strings.Contains(res.Err, "cwd") {
		t.Fatalf("非法 cwd 应报错: %+v", res)
	}
	if res.TimedOut || res.Interrupted || res.NotStarted {
		t.Errorf("快速失败不应标记中断/超时/未启动: %+v", res)
	}
}

func TestShellToolConcurrentRun(t *testing.T) {
	tool := testShellTool(t)
	results := make([]string, 8)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := fmt.Sprintf("echo out-%d; exit %d", i, i)
			results[i] = tool.run(context.Background(), shellRequest{Command: cmd, TimeoutSec: 30}).String()
		}(i)
	}
	wg.Wait()
	for i, got := range results {
		if !strings.Contains(got, fmt.Sprintf("out-%d", i)) {
			t.Errorf("并发第 %d 次结果串扰: %q", i, got)
		}
		if i > 0 && !strings.Contains(got, fmt.Sprintf("exit code: %d", i)) {
			t.Errorf("并发第 %d 次退出码丢失: %q", i, got)
		}
	}
}
