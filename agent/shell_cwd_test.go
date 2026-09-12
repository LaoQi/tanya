package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type dirRecBridge struct {
	fakeTTYBridge
	dir string
}

func (b *dirRecBridge) Prepare(cmd *exec.Cmd) (*os.File, error) {
	b.dir = cmd.Dir
	return b.fakeTTYBridge.Prepare(cmd)
}

func shellStdout(res *ShellResult) string {
	var b strings.Builder
	for _, c := range res.Stdout {
		b.WriteString(c.Data)
	}
	return b.String()
}

func TestResolveShellCwd(t *testing.T) {
	home, _ := os.UserHomeDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, want string }{
		{"", ""},
		{"/tmp", "/tmp"},
		{"~", home},
		{"~/", home},
		{".", cwd},
		{"..", filepath.Dir(cwd)},
	}
	for _, c := range cases {
		got, err := resolveShellCwd(c.in, cwd)
		if err != nil || got != c.want {
			t.Errorf("resolveShellCwd(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"/no/such/dir-tanyan", file} {
		if got, err := resolveShellCwd(bad, cwd); err == nil {
			t.Errorf("resolveShellCwd(%q) 应报错，得到 %q", bad, got)
		}
	}
}

func TestRunShellCwdDefault(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	res := RunShellResult(context.Background(), "pwd", 10, false, "", "")
	if got := strings.TrimSpace(shellStdout(res)); got != cwd {
		t.Errorf("默认目录应为进程 cwd: got %q want %q", got, cwd)
	}
	if res.Cwd != "" {
		t.Errorf("未指定 cwd 时不应填充 Cwd: %q", res.Cwd)
	}
	if strings.Contains(res.String(), "cwd: ") {
		t.Errorf("未指定 cwd 时结果不应含 cwd 行: %q", res.String())
	}
}

func TestRunShellCwdEffective(t *testing.T) {
	dir := t.TempDir()
	res := RunShellResult(context.Background(), "pwd", 10, false, dir, "")
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(shellStdout(res)))
	if err != nil || got != want {
		t.Fatalf("cwd 未生效: got %q (%v) want %q; res=%+v", got, err, want, res)
	}
	if res.Cwd != dir {
		t.Errorf("Cwd 字段: got %q want %q", res.Cwd, dir)
	}
	if !strings.Contains(res.String(), "cwd: "+dir+"\n") {
		t.Errorf("结果应含 cwd 行: %q", res.String())
	}
}

func TestRunShellCwdRelative(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	res := RunShellResult(context.Background(), "pwd", 10, false, ".", cwd)
	if res.Cwd != cwd {
		t.Errorf("相对路径应按会话启动目录解析: got %q want %q", res.Cwd, cwd)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(shellStdout(res)))
	if err != nil || got != cwd {
		t.Errorf("执行目录: got %q (%v) want %q", got, err, cwd)
	}
}

func TestRunShellCwdMissing(t *testing.T) {
	base := t.TempDir()
	marker := filepath.Join(base, "marker")
	missing := filepath.Join(base, "nope")
	res := RunShellResult(context.Background(), "touch "+marker, 10, false, missing, "")
	if !strings.Contains(res.Err, "cwd") {
		t.Errorf("应报 cwd 错误: %q", res.Err)
	}
	if res.Interrupted || res.TimedOut || res.NotStarted {
		t.Errorf("快速失败不应标记中断/超时/未启动: %+v", res)
	}
	if len(res.Stdout) != 0 || len(res.Stderr) != 0 {
		t.Errorf("不应产生输出: %+v", res)
	}
	if !strings.Contains(res.String(), "error: ") {
		t.Errorf("结果应含错误行: %q", res.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("cwd 非法时命令不应被执行")
	}
}

func TestRunShellInteractiveCwd(t *testing.T) {
	b := &dirRecBridge{}
	withFakeBridge(t, b)
	dir := t.TempDir()
	res := RunShellResult(context.Background(), "echo hi", 10, true, dir, "")
	if b.dir != dir {
		t.Errorf("桥接子进程 dir = %q want %q", b.dir, dir)
	}
	if res.Cwd != dir {
		t.Errorf("Cwd = %q want %q", res.Cwd, dir)
	}
	if !b.prepared || !b.attached {
		t.Errorf("桥接未走全流程: %+v", b)
	}
}

func TestAskShellToolCwd(t *testing.T) {
	dir := t.TempDir()
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "run_shell", args: `{"command":"pwd","cwd":"` + dir + `"}`}}},
		mockStep{content: "完成"},
	)
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	sink := EventSink(func(Event) {})
	if err := a.Ask(context.Background(), "在指定目录执行", sink); err != nil {
		t.Fatal(err)
	}
	if len(a.history) != 4 {
		t.Fatalf("history: %d", len(a.history))
	}
	content := a.history[2].Content
	if !strings.Contains(content, "cwd: "+dir) {
		t.Errorf("tool 结果应含 cwd 行: %q", content)
	}
}

func TestResolveShellCwdWorkspaceBase(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveShellCwd("sub", ws)
	if err != nil || got != sub {
		t.Fatalf("相对路径应按工作区合成: got %q, %v; want %q", got, err, sub)
	}
	if got, err := resolveShellCwd("sub", ""); err == nil || got != "" {
		t.Errorf("缺工作区基准时相对路径应报错: got %q, %v", got, err)
	}
}

func TestRunShellCwdRelativeToWorkspace(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	res := RunShellResult(context.Background(), "pwd", 10, false, "sub", ws)
	if res.Cwd != sub {
		t.Fatalf("Cwd = %q want %q", res.Cwd, sub)
	}
	want, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(shellStdout(res)))
	if err != nil || got != want {
		t.Fatalf("命令未在工作区子目录执行: got %q (%v) want %q; res=%+v", got, err, want, res)
	}
}

func TestAskShellToolRelativeCwdUsesWorkspace(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "run_shell", args: `{"command":"pwd","cwd":"."}`}}},
		mockStep{content: "完成"},
	)
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	sink := EventSink(func(Event) {})
	if err := a.Ask(context.Background(), "在相对目录执行", sink); err != nil {
		t.Fatal(err)
	}
	if len(a.history) != 4 {
		t.Fatalf("history: %d", len(a.history))
	}
	if content := a.history[2].Content; !strings.Contains(content, "cwd: "+cwd+"\n") {
		t.Errorf("相对 cwd 应按工作区（a.cwd）解析: %q", content)
	}
}
