package agent

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func evalDir(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestSwitchWorkspaceResetsEverything(t *testing.T) {
	a := newTestAgent(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "AGENTS.md"), []byte("项目说明：新的工作区标记"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.history = []Message{{Role: "user", Content: "旧会话内容"}}
	oldPath := a.store.path()

	if err := a.SwitchWorkspace(target); err != nil {
		t.Fatalf("切换失败: %v", err)
	}
	if a.Workspace() != target {
		t.Errorf("工作区: got %q want %q", a.Workspace(), target)
	}
	if len(a.History()) != 0 {
		t.Errorf("切换应放弃当前会话，历史应清空: %+v", a.History())
	}
	if a.SessionID() == "" || a.store.path() == oldPath {
		t.Errorf("会话应轮转为新文件: got %q old %q", a.store.path(), oldPath)
	}
	if got := a.store.workspace; got != target {
		t.Errorf("store 工作区应跟随: got %q", got)
	}
	if got := a.systemPrompt(); !strings.Contains(got, "新的工作区标记") {
		t.Errorf("system 提示应重读新工作区的 AGENTS.md: %q", got)
	}
	if got := a.runtimePrompt(); !strings.Contains(got, "CWD: "+shortPath(target)) {
		t.Errorf("env 段应指向新工作区: %q", got)
	}
	res := a.dispatch(context.Background(), "run_shell", `{"command":"pwd"}`)
	sh, ok := res.Meta.(*ShellResult)
	if !ok || sh == nil {
		t.Fatalf("run_shell 未返回: %+v", res)
	}
	if got, want := evalDir(t, strings.TrimSpace(shellStdout(sh))), evalDir(t, target); got != want {
		t.Errorf("run_shell 默认目录应跟随工作区: got %q want %q", got, want)
	}
	if sh.Cwd != "" {
		t.Errorf("默认目录不应回显 cwd: %q", sh.Cwd)
	}
}

func TestSwitchWorkspaceSessionsFollowTarget(t *testing.T) {
	a := newTestAgent(t)
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(local, ".tanya"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.SwitchWorkspace(local); err != nil {
		t.Fatal(err)
	}
	dir, ok := a.SessionDir()
	if want := filepath.Join(local, ".tanya", "sessions"); !ok || dir != want {
		t.Errorf("auto 模式应落目标工作区的 .tanya: got %q ok=%v want %q", dir, ok, want)
	}

	plain := t.TempDir()
	if err := a.SwitchWorkspace(plain); err != nil {
		t.Fatal(err)
	}
	dir, ok = a.SessionDir()
	want := filepath.Join(a.cfg.DataDir, "workspaces", workspaceID(plain), "sessions")
	if !ok || dir != want {
		t.Errorf("无 .tanya 的目标应落 data_dir 工作区: got %q ok=%v want %q", dir, ok, want)
	}
}

func TestSwitchWorkspaceRejects(t *testing.T) {
	a := newTestAgent(t)
	oldWorkspace := a.Workspace()
	oldID := a.SessionID()
	a.history = []Message{{Role: "user", Content: "保留"}}
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []string{file, filepath.Join(t.TempDir(), "missing"), "", "   ", oldWorkspace, filepath.Join(oldWorkspace, ".")}
	for _, target := range cases {
		if err := a.SwitchWorkspace(target); err == nil {
			t.Errorf("%q 应被拒绝", target)
		}
	}
	if a.Workspace() != oldWorkspace || a.SessionID() != oldID || len(a.History()) != 1 {
		t.Errorf("拒绝后状态应不变: ws=%q id=%q msgs=%d", a.Workspace(), a.SessionID(), len(a.History()))
	}
}

func TestSwitchWorkspacePathForms(t *testing.T) {
	a := newTestAgent(t)
	sub := filepath.Join(a.Workspace(), "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.SwitchWorkspace("sub"); err != nil {
		t.Fatalf("相对路径应按当前工作区解析: %v", err)
	}
	if a.Workspace() != sub {
		t.Errorf("相对路径: got %q want %q", a.Workspace(), sub)
	}
	if err := a.SwitchWorkspace(sub + string(filepath.Separator)); err == nil {
		t.Error("等价路径应识别为同一工作区并拒绝")
	}

	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("无 HOME")
	}
	tilde := filepath.Join(home, "ws-tilde")
	if err := os.Mkdir(tilde, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.SwitchWorkspace("~/ws-tilde"); err != nil {
		t.Fatalf("~ 展开失败: %v", err)
	}
	if a.Workspace() != tilde {
		t.Errorf("~ 展开: got %q want %q", a.Workspace(), tilde)
	}
	if err := a.SwitchWorkspace("~"); err != nil {
		t.Fatalf("~ 应展开家目录: %v", err)
	}
	if a.Workspace() != home {
		t.Errorf("~: got %q want %q", a.Workspace(), home)
	}
}

func TestSwitchWorkspaceNoSave(t *testing.T) {
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	a, err := New(cfg, NoSave(true))
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := a.SwitchWorkspace(target); err != nil {
		t.Fatal(err)
	}
	if !a.NoSave() {
		t.Error("不落盘状态应保持")
	}
	if _, ok := a.SessionDir(); ok {
		t.Error("不落盘模式不应有会话目录")
	}
	if _, err := os.Stat(filepath.Join(target, ".tanya")); !os.IsNotExist(err) {
		t.Errorf("不落盘模式不应创建 .tanya: %v", err)
	}
}

func TestSwitchWorkspaceAtomicOnBuildFailure(t *testing.T) {
	a := newTestAgent(t)
	from := a.Workspace()
	oldID := a.SessionID()
	a.history = []Message{{Role: "user", Content: "保留"}}

	blocked := t.TempDir()
	if err := os.MkdirAll(filepath.Join(blocked, ".tanya"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, ".tanya", "sessions"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.SwitchWorkspace(blocked); err == nil {
		t.Error("会话目录建不出来时应失败")
	}

	broken := t.TempDir()
	shell := a.cfg.Shell
	a.cfg.Shell = "tanya-no-such-shell"
	if err := a.SwitchWorkspace(broken); err == nil {
		t.Error("shell 解析失败时应失败")
	}
	a.cfg.Shell = shell

	if a.Workspace() != from || a.SessionID() != oldID || len(a.History()) != 1 {
		t.Fatalf("失败后状态应不变: ws=%q id=%q msgs=%d", a.Workspace(), a.SessionID(), len(a.History()))
	}
	if err := a.SwitchWorkspace(broken); err != nil {
		t.Fatalf("恢复配置后应能切换: %v", err)
	}
	if a.Workspace() != broken {
		t.Errorf("工作区: got %q want %q", a.Workspace(), broken)
	}
}

func TestSwitchWorkspaceWithoutHome(t *testing.T) {
	a := newTestAgent(t)
	a.home = ""
	for _, dir := range []string{"~", "~/anywhere"} {
		if err := a.SwitchWorkspace(dir); err == nil {
			t.Errorf("无家目录基准时 %q 应报错", dir)
		}
	}
	if a.Workspace() == "" {
		t.Error("失败不应清空工作区")
	}
}

func TestSwitchWorkspaceThenArchiveComment(t *testing.T) {
	a := newTestAgent(t)
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, ".tanya"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.SwitchWorkspace(target); err != nil {
		t.Fatal(err)
	}
	dir, ok := a.SessionDir()
	if !ok {
		t.Fatal("应有会话目录")
	}
	now := time.Now()
	seedSession(t, dir, "20260101-090000", `{"role":"user","content":"旧会话"}`+"\n", now.Add(-time.Hour))

	rep, err := a.ArchiveSessions(ArchiveOptions{Exclude: "keep-current", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sessions) != 1 {
		t.Fatalf("应有 1 个归档条目: %+v", rep.Sessions)
	}
	zr, err := zip.OpenReader(rep.Volume)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var vc archiveVolumeComment
	if err := json.Unmarshal([]byte(zr.Comment), &vc); err != nil {
		t.Fatalf("卷注释不可解析: %q %v", zr.Comment, err)
	}
	if vc.Workspace != target {
		t.Errorf("卷注释应记当前工作区: got %q want %q", vc.Workspace, target)
	}
}
