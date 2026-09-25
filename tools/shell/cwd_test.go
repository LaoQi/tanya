package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type dirRecBridge struct {
	fakeBridge
	dir string
}

func (b *dirRecBridge) Prepare(cmd *exec.Cmd) (*os.File, error) {
	b.dir = cmd.Dir
	return b.fakeBridge.Prepare(cmd)
}

func shellStdout(res *Result) string {
	var b strings.Builder
	for _, c := range res.Stdout {
		b.WriteString(c.Data)
	}
	return b.String()
}

func TestRunShellCwdDefault(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	res := testShellTool(t).run(context.Background(), request{Command: "pwd", TimeoutSec: 10})
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
	res := testShellTool(t).run(context.Background(), request{Command: "pwd", TimeoutSec: 10, Cwd: dir})
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
	res := testShellTool(t, func(cfg *Config) { cfg.Workspace = func() string { return cwd } }).run(context.Background(), request{Command: "pwd", TimeoutSec: 10, Cwd: "."})
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
	res := testShellTool(t).run(context.Background(), request{Command: "touch " + marker, TimeoutSec: 10, Cwd: missing})
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
	dir := t.TempDir()
	res := bridgeTool(t, b).run(context.Background(), request{Command: "echo hi", TimeoutSec: 10, Interactive: true, Cwd: dir})
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

func TestRunShellCwdRelativeToWorkspace(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	res := testShellTool(t, func(cfg *Config) { cfg.Workspace = func() string { return ws } }).run(context.Background(), request{Command: "pwd", TimeoutSec: 10, Cwd: "sub"})
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

func TestRunShellCwdKeepsProcessCwd(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res := testShellTool(t).run(context.Background(), request{Command: "pwd", TimeoutSec: 10, Cwd: dir})
	if res.Err != "" || res.ExitCode != 0 {
		t.Fatalf("run: %+v", res)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("进程 cwd 被改变: %q → %q", before, after)
	}
}

func TestWorkspaceConsultedPerInvoke(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	ws := first
	tool := testShellTool(t, func(c *Config) { c.Workspace = func() string { return ws } })
	pwd := func() string {
		res := tool.run(context.Background(), request{Command: "pwd", TimeoutSec: 10})
		return evalDirTrim(t, shellStdout(res))
	}
	if got, want := pwd(), evalDirTrim(t, first); got != want {
		t.Fatalf("默认目录应取当时的工作区: got %q want %q", got, want)
	}
	ws = second
	if got, want := pwd(), evalDirTrim(t, second); got != want {
		t.Fatalf("工作区变化后默认目录应跟随: got %q want %q", got, want)
	}
}

func evalDirTrim(t *testing.T, p string) string {
	t.Helper()
	p = strings.TrimSpace(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
