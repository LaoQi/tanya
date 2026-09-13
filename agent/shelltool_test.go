package agent

import (
	"context"
	"errors"
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
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("UserHomeDir 不可用: err=%v home=%q", err, home)
	}
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

func TestNewShellToolEmptyPrograms(t *testing.T) {
	calls := 0
	lookPath := func(name string) (string, error) {
		calls++
		if name == "bash" {
			return "/usr/bin/bash", nil
		}
		return "", errors.New("not found")
	}
	tool, err := newShellTool(shellToolConfig{GOOS: "linux", LookPath: lookPath, Programs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if tool.programs == nil || len(tool.programs) != 0 {
		t.Errorf("空切片应表示显式无程序: %#v", tool.programs)
	}
	if calls != 1 {
		t.Errorf("空切片应跳过程序探针: LookPath 调用 %d 次", calls)
	}
	if desc := tool.toolDesc(); strings.Contains(desc, "可用程序") {
		t.Errorf("无程序时不应追加可用程序段: %q", desc)
	}
}

func TestShellToolInteractiveBadCwdSkipsBridge(t *testing.T) {
	f := &fakeTTYBridge{}
	res := bridgeTool(t, f).run(context.Background(), shellRequest{
		Command:     "echo hi",
		TimeoutSec:  10,
		Interactive: true,
		Cwd:         filepath.Join(t.TempDir(), "nope"),
	})
	if !strings.Contains(res.Err, "cwd") {
		t.Fatalf("非法 cwd 应快速失败: %+v", res)
	}
	if f.prepared || f.attached {
		t.Errorf("非法 cwd 不应触碰终端租约: prepared=%v attached=%v", f.prepared, f.attached)
	}
}

func TestShellToolDescGolden(t *testing.T) {
	tool := &shellTool{
		profile:  &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix},
		programs: []string{"ls", "grep"},
	}
	want := "在 " + runtime.GOOS + " bash 中执行 shell 命令，返回 stdout/stderr/退出码。读文件、搜索、文本处理等系统操作都用它。" +
		"默认在会话启动目录（进程 cwd）下执行，无需 cd 进入项目；需要其它目录时用 cwd 参数，不必写 cd 前缀。" +
		"可用程序: ls, grep"
	if got := tool.toolDesc(); got != want {
		t.Errorf("toolDesc 全串不匹配:\n got %q\nwant %q", got, want)
	}
}

func TestRunShellParamsGolden(t *testing.T) {
	want := `{"type":"object","properties":{"command":{"type":"string","description":"要执行的命令"},"cwd":{"type":"string","description":"命令执行目录，默认会话启动目录"},"timeout":{"type":"integer","description":"超时秒数，默认 60（interactive 时 300），最大 900"},"interactive":{"type":"boolean","description":"命令需要用户在终端应答（sudo/ssh/gpg/read 等交互提示）时置 true：命令在独立 pty 中运行、终端直通应答，停用等待动画，默认超时放宽"}},"required":["command"]}`
	if got := runShellParams(); got != want {
		t.Errorf("runShellParams 全串不匹配:\n got %q\nwant %q", got, want)
	}
}
