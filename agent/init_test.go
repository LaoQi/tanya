package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initConfig(t *testing.T) *Config {
	t.Helper()
	cfg := defaultConfig()
	cfg.DataDir = filepath.Join(t.TempDir(), "global")
	return cfg
}

func entryOf(t *testing.T, rep *InitReport, rel string) InitEntry {
	t.Helper()
	path := filepath.Join(rep.Workspace, filepath.FromSlash(rel))
	for _, e := range rep.Entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("报告缺少条目 %s: %+v", rel, rep.Entries)
	return InitEntry{}
}

func initYes() InitOptions { return InitOptions{ConfirmIgnore: func() bool { return true }} }

func TestInitWorkspaceFresh(t *testing.T) {
	isolatePromptEnv(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := InitWorkspace(initConfig(t), initYes())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Workspace != cwd {
		t.Errorf("工作区: %q want %q", rep.Workspace, cwd)
	}
	if rep.SessionDir != filepath.Join(cwd, ".tanya", "sessions") {
		t.Errorf("会话目录应落在新建的工作区标记内: %q", rep.SessionDir)
	}
	for _, rel := range []string{".tanya/sessions", ".tanya/.gitignore", "AGENTS.md"} {
		if e := entryOf(t, rep, rel); e.Action != InitCreated {
			t.Errorf("%s 应新建: %+v", rel, e)
		}
	}
	if fi, err := os.Stat(filepath.Join(cwd, ".tanya", "sessions")); err != nil || !fi.IsDir() {
		t.Errorf(".tanya/sessions 应为目录: %v %v", fi, err)
	}
	b, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "# "+filepath.Base(cwd)+"\n") {
		t.Errorf("骨架应以目录名为标题: %q", string(b))
	}
	if !strings.Contains(string(b), "## 项目说明") || !strings.Contains(string(b), "## 构建与测试") {
		t.Errorf("骨架缺少小节: %q", string(b))
	}
	ig, err := os.ReadFile(filepath.Join(cwd, ".tanya", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(ig) != ignoreFileContent {
		t.Errorf("忽略文件内容: %q", string(ig))
	}
}

func TestInitWorkspaceIdempotent(t *testing.T) {
	isolatePromptEnv(t)
	cwd, _ := os.Getwd()
	cfg := initConfig(t)
	first, err := InitWorkspace(cfg, initYes())
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := InitWorkspace(cfg, initYes())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range rep.Entries {
		if e.Action != InitExists {
			t.Errorf("二次运行应全部跳过创建: %+v", e)
		}
	}
	after, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("二次运行不应改写 AGENTS.md")
	}
	if first.SessionDir != rep.SessionDir {
		t.Errorf("会话目录应稳定: %q → %q", first.SessionDir, rep.SessionDir)
	}
}

func TestInitKeepsExistingAgents(t *testing.T) {
	isolatePromptEnv(t)
	cwd, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte("哨兵\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := InitWorkspace(initConfig(t), initYes())
	if err != nil {
		t.Fatal(err)
	}
	if e := entryOf(t, rep, "AGENTS.md"); e.Action != InitExists {
		t.Errorf("已有 AGENTS.md 应只报告: %+v", e)
	}
	b, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "哨兵\n" {
		t.Errorf("已有内容不得改动: %q", string(b))
	}
	if e := entryOf(t, rep, ".tanya/sessions"); e.Action != InitCreated {
		t.Errorf("仍应建立工作区标记: %+v", e)
	}
}

func TestInitIgnoreSkipped(t *testing.T) {
	cases := []struct {
		name string
		o    InitOptions
		want string
	}{
		{"非交互", InitOptions{}, MsgInitSkipNoTTY},
		{"用户拒绝", InitOptions{ConfirmIgnore: func() bool { return false }}, MsgInitSkipDeclined},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolatePromptEnv(t)
			cwd, _ := os.Getwd()
			rep, err := InitWorkspace(initConfig(t), c.o)
			if err != nil {
				t.Fatal(err)
			}
			e := entryOf(t, rep, ".tanya/.gitignore")
			if e.Action != InitSkipped || e.Reason != c.want {
				t.Errorf("应报告跳过与原因: %+v", e)
			}
			if _, err := os.Stat(filepath.Join(cwd, ".tanya", ".gitignore")); !os.IsNotExist(err) {
				t.Errorf("未确认时不应落盘: %v", err)
			}
		})
	}
}

func TestInitPartialWorkspace(t *testing.T) {
	isolatePromptEnv(t)
	cwd, _ := os.Getwd()
	if err := os.MkdirAll(filepath.Join(cwd, ".tanya", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	rep, err := InitWorkspace(initConfig(t), initYes())
	if err != nil {
		t.Fatal(err)
	}
	if e := entryOf(t, rep, ".tanya/sessions"); e.Action != InitExists {
		t.Errorf("已有会话目录应只报告: %+v", e)
	}
	if e := entryOf(t, rep, "AGENTS.md"); e.Action != InitCreated {
		t.Errorf("缺 AGENTS.md 时应补齐: %+v", e)
	}
}

func TestInitFailOnBlockedPaths(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		want string
		dir  bool
	}{
		{"AGENTS.md 是目录", "AGENTS.md", MsgInitNotFile, true},
		{"sessions 是文件", ".tanya/sessions", MsgInitNotDir, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolatePromptEnv(t)
			cwd, _ := os.Getwd()
			path := filepath.Join(cwd, filepath.FromSlash(c.rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			var err error
			if c.dir {
				err = os.Mkdir(path, 0o755)
			} else {
				err = os.WriteFile(path, []byte("x"), 0o644)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = InitWorkspace(initConfig(t), initYes())
			if err == nil {
				t.Fatal("应报错")
			}
			if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("错误应指明路径与原因: %v", err)
			}
		})
	}
}

func TestInitThenNewUsesLocalSession(t *testing.T) {
	isolatePromptEnv(t)
	cwd, _ := os.Getwd()
	cfg := initConfig(t)
	if _, err := InitWorkspace(cfg, initYes()); err != nil {
		t.Fatal(err)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if a.store.dir != filepath.Join(cwd, ".tanya", "sessions") {
		t.Errorf("init 后 auto 应落本地工作区: %q", a.store.dir)
	}
	p := a.systemPrompt()
	if !strings.Contains(p, "# 项目说明（AGENTS.md）") || !strings.Contains(p, "## 构建与测试") {
		t.Errorf("本次会话的 system prompt 应已含新骨架:\n%s", p)
	}
}

func TestInitExplicitGlobalKeepsMarker(t *testing.T) {
	isolatePromptEnv(t)
	cfg := initConfig(t)
	cfg.SessionMode = "global"
	rep, err := InitWorkspace(cfg, initYes())
	if err != nil {
		t.Fatal(err)
	}
	if rep.SessionDir != filepath.Join(cfg.DataDir, "workspaces", workspaceID(rep.Workspace), "sessions") {
		t.Errorf("显式 global 应如实报告落点: %q", rep.SessionDir)
	}
	if e := entryOf(t, rep, ".tanya/sessions"); e.Action != InitCreated {
		t.Errorf("工作区标记仍应创建: %+v", e)
	}
}
