package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolatePromptEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})
}

const testBasePrompt = "BASE"

func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	a, err := New(cfg, WithSystemPrompt(testBasePrompt))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestHistoryAccessor(t *testing.T) {
	a := newTestAgent(t)
	if len(a.History()) != 0 {
		t.Error("新会话应为空")
	}
	a.history = []Message{{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"}}
	h := a.History()
	if len(h) != 2 || h[0].Content != "q1" || h[1].Role != "assistant" {
		t.Errorf("History 异常: %+v", h)
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := estimateTokens("你好"); got != 2 {
		t.Errorf("中文: got %d", got)
	}
	if got := estimateTokens("abc"); got != 1 {
		t.Errorf("英文: got %d", got)
	}
	if got := estimateTokens(""); got != 0 {
		t.Errorf("空: got %d", got)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	a := newTestAgent(t)
	a.history = []Message{
		{Role: "user", Content: "问题一"},
		{Role: "assistant", Content: "回答一"},
		{Role: "user", Content: "问题二"},
		{Role: "assistant", Content: "回答二"},
	}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSuffix(filepath.Base(a.store.path()), ".jsonl")

	b := newTestAgent(t)
	b.store.dir = a.store.dir
	if err := b.LoadSession(id); err != nil {
		t.Fatal(err)
	}
	if len(b.history) != 4 {
		t.Fatalf("载入消息数: %d", len(b.history))
	}
	for i := range a.history {
		if a.history[i].Content != b.history[i].Content || a.history[i].Role != b.history[i].Role {
			t.Errorf("第 %d 条不一致", i)
		}
	}

	b.history = append(b.history, Message{Role: "user", Content: "问题三"})
	if err := b.save(); err != nil {
		t.Fatal(err)
	}
	c := newTestAgent(t)
	c.store.dir = a.store.dir
	if err := c.LoadSession(id); err != nil {
		t.Fatal(err)
	}
	if len(c.history) != 5 {
		t.Errorf("追加保存后应为 5 条: %d", len(c.history))
	}
}

func TestLoadSessionInvalid(t *testing.T) {
	a := newTestAgent(t)
	if err := a.LoadSession("../etc/passwd"); err == nil {
		t.Error("路径穿越应被拒绝")
	}
	if err := a.LoadSession("not-exist"); err == nil {
		t.Error("不存在的会话应报错")
	}
}

func TestNewSessionResetsUsage(t *testing.T) {
	a := newTestAgent(t)
	a.stats.record(&Usage{PromptTokens: 1200})
	a.NewSession()
	if a.stats.hasContext {
		t.Error("/new 应清理上次用量")
	}
}

func TestLoadSessionResetsUsage(t *testing.T) {
	a := newTestAgent(t)
	p := filepath.Join(a.store.dir, "20260101-090000.jsonl")
	if err := os.WriteFile(p, []byte(`{"role":"user","content":"历史会话"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.stats.record(&Usage{PromptTokens: 1200})
	if err := a.LoadSession("20260101-090000"); err != nil {
		t.Fatal(err)
	}
	if a.stats.hasContext {
		t.Error("/load 应清理上次用量")
	}
}

func TestListSessions(t *testing.T) {
	a := newTestAgent(t)
	write := func(name, content string) {
		p := filepath.Join(a.store.dir, name+".jsonl")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("20260101-100000", `{"role":"user","content":"第一个会话"}`+"\n"+`{"role":"assistant","content":"好"}`+"\n")
	write("20260102-100000", `{"role":"user","content":"第二个会话"}`+"\n")

	list, err := a.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("会话数: %d", len(list))
	}
	if list[0].ID != "20260102-100000" {
		t.Errorf("应按 id 倒序: %s", list[0].ID)
	}
	if list[1].Msgs != 2 || list[1].Summary != "第一个会话" {
		t.Errorf("会话信息异常: %+v", list[1])
	}
}

func TestListSessionsIncrementalRefresh(t *testing.T) {
	a := newTestAgent(t)
	p := filepath.Join(a.store.dir, "20260101-100000.jsonl")
	if err := os.WriteFile(p, []byte(`{"role":"user","content":"第一条"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListSessions()
	if err != nil || len(list) != 1 || list[0].Msgs != 1 {
		t.Fatalf("初始列表异常: %+v %v", list, err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"role":"assistant","content":"好"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	list, err = a.ListSessions()
	if err != nil || list[0].Msgs != 2 {
		t.Errorf("追加后条数应刷新: %+v %v", list, err)
	}
	if err := os.WriteFile(filepath.Join(a.store.dir, "20260102-100000.jsonl"), []byte(`{"role":"user","content":"第二条"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err = a.ListSessions()
	if err != nil || len(list) != 2 {
		t.Errorf("新文件应立即可见: %+v %v", list, err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	list, err = a.ListSessions()
	if err != nil || len(list) != 1 || list[0].ID != "20260102-100000" {
		t.Errorf("删除后应从列表移除: %+v %v", list, err)
	}
}

func TestWorkspaceID(t *testing.T) {
	id := workspaceID("/home/coco/Project/tanya")
	if !strings.HasPrefix(id, "home-coco-Project-tanya-") {
		t.Errorf("可读前缀异常: %q", id)
	}
	suffix := strings.TrimPrefix(id, "home-coco-Project-tanya-")
	if len(suffix) != 8 {
		t.Errorf("短哈希应为 8 位: %q", id)
	}
	if id != workspaceID("/home/coco/Project/tanya") {
		t.Error("同一路径应生成相同 id")
	}
	if workspaceID("/a/b-c") == workspaceID("/a-b/c") {
		t.Error("碰撞路径的哈希应不同")
	}
	if got := workspaceID("/"); !strings.HasPrefix(got, "root-") {
		t.Errorf("根目录: %q", got)
	}
}

func TestNewSessionPerWorkspace(t *testing.T) {
	a := newTestAgent(t)
	base := filepath.Join(a.cfg.DataDir, "workspaces", workspaceID(a.workspace))
	if a.store.dir != filepath.Join(base, "sessions") {
		t.Errorf("sessionDir 应为 <data_dir>/workspaces/<id>/sessions: %q", a.store.dir)
	}
	if _, err := os.Stat(a.store.dir); err != nil {
		t.Errorf("工作区目录未创建: %v", err)
	}
}

func TestResolveWorkspaceDirs(t *testing.T) {
	root := t.TempDir()
	cfg := defaultConfig()
	cfg.DataDir = filepath.Join(root, "data")

	globalSessions := filepath.Join(cfg.DataDir, "workspaces", workspaceID(root), "sessions")
	globalArchive := filepath.Join(cfg.DataDir, "workspaces", workspaceID(root), "archive")
	sessions, archive := resolveWorkspaceDirs(cfg, root)
	if sessions != globalSessions || archive != globalArchive {
		t.Errorf("auto 无 .tanya 应走 global: %q %q", sessions, archive)
	}

	if err := os.MkdirAll(filepath.Join(root, ".tanya"), 0o755); err != nil {
		t.Fatal(err)
	}
	localSessions := filepath.Join(root, ".tanya", "sessions")
	localArchive := filepath.Join(root, ".tanya", "archive")
	sessions, archive = resolveWorkspaceDirs(cfg, root)
	if sessions != localSessions || archive != localArchive {
		t.Errorf("auto 有 .tanya 应走 local: %q %q", sessions, archive)
	}
	cfg.SessionMode = "global"
	if sessions, archive = resolveWorkspaceDirs(cfg, root); sessions != globalSessions || archive != globalArchive {
		t.Errorf("global 显式指定应优先: %q %q", sessions, archive)
	}
	cfg.SessionMode = "local"
	if sessions, archive = resolveWorkspaceDirs(cfg, root); sessions != localSessions || archive != localArchive {
		t.Errorf("local 显式指定: %q %q", sessions, archive)
	}
	cfg.SessionMode = "bogus"
	if sessions, archive = resolveWorkspaceDirs(cfg, root); sessions != localSessions || archive != localArchive {
		t.Errorf("未知模式应按 auto 处理: %q %q", sessions, archive)
	}
}

func TestNewLocalMode(t *testing.T) {
	isolatePromptEnv(t)
	tmp, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.DataDir = filepath.Join(tmp, "global")
	cfg.SessionMode = "local"
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(a.store.dir, filepath.Join(".tanya", "sessions")) {
		t.Errorf("local 模式目录: %q", a.store.dir)
	}
	if _, err := os.Stat(a.store.dir); err != nil {
		t.Errorf("目录未创建: %v", err)
	}
}

func TestStatsSnapshot(t *testing.T) {
	a := newTestAgent(t)
	a.history = append(a.history, Message{Role: "user", Content: "hi"})
	a.stats.record(&Usage{PromptTokens: 1200, CompletionTokens: 30, TotalTokens: 1230, CacheHitTokens: 900})
	a.stats.record(&Usage{PromptTokens: 2000, CompletionTokens: 40, TotalTokens: 2040, CacheHitTokens: 1500})
	st := a.Stats()
	wd, _ := os.Getwd()
	if st.Workspace != wd || st.Session != a.store.path() || st.Messages != 1 {
		t.Errorf("会话元数据异常: %+v", st)
	}
	if !st.HasContext || st.ContextTokens != 2000 || st.ContextHit != 1500 {
		t.Errorf("上下文与单次命中量应取最近一次实报: %+v", st)
	}
	if st.PromptTokens != 3200 || st.CompletionTokens != 70 || st.TotalTokens != 3270 || st.CacheHitTokens != 2400 {
		t.Errorf("累计项应按全部请求求和: %+v", st)
	}
	if st.Est <= 0 {
		t.Errorf("估算值应可用: %+v", st)
	}
}

func TestSetModel(t *testing.T) {
	a := newTestAgent(t)
	if err := a.SetModel("new-model"); err != nil {
		t.Fatal(err)
	}
	if a.Model() != "new-model" {
		t.Error("模型切换失败")
	}
	if err := a.SetModel("   "); err == nil {
		t.Error("空白模型名应报错")
	}
}

func TestSetReasoningEffort(t *testing.T) {
	a := newTestAgent(t)
	if err := a.SetReasoningEffort("HIGH"); err != nil {
		t.Fatal(err)
	}
	if a.ReasoningEffort() != "high" {
		t.Errorf("应归一为小写: %q", a.ReasoningEffort())
	}
	if err := a.SetReasoningEffort("max"); err != nil {
		t.Fatal(err)
	}
	if a.ReasoningEffort() != "max" {
		t.Errorf("max 等级未生效: %q", a.ReasoningEffort())
	}
	if err := a.SetReasoningEffort(" off "); err != nil {
		t.Fatal(err)
	}
	if a.ReasoningEffort() != "" {
		t.Errorf("off 应清空: %q", a.ReasoningEffort())
	}
	if err := a.SetReasoningEffort("bogus"); err == nil {
		t.Fatal("非法值应报错")
	}
	if a.ReasoningEffort() != "" {
		t.Errorf("失败后不应变更: %q", a.ReasoningEffort())
	}
}

func writeAgents(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSystemPromptAgents(t *testing.T) {
	a := newTestAgent(t)
	if a.systemPrompt() != testBasePrompt {
		t.Errorf("无 AGENTS.md 应仅注入的默认提示: %q", a.systemPrompt())
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeAgents(t, filepath.Join(cwd, "AGENTS.md"), "项目规则 A\n")
	a.NewSession()
	want := testBasePrompt + "\n\n# 项目说明（AGENTS.md）\n\n项目规则 A"
	if a.systemPrompt() != want {
		t.Errorf("工作区注入异常: %q", a.systemPrompt())
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "tanya"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeAgents(t, filepath.Join(home, ".config", "tanya", "AGENTS.md"), "全局规则 G")
	a.NewSession()
	want = testBasePrompt +
		"\n\n# 全局说明（~/.config/tanya/AGENTS.md）\n\n全局规则 G" +
		"\n\n# 项目说明（AGENTS.md）\n\n项目规则 A"
	if a.systemPrompt() != want {
		t.Errorf("双层组装异常: %q", a.systemPrompt())
	}
}

func TestSystemPromptBlankFile(t *testing.T) {
	a := newTestAgent(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeAgents(t, filepath.Join(cwd, "AGENTS.md"), "  \n\t\n")
	a.NewSession()
	if a.systemPrompt() != testBasePrompt {
		t.Errorf("空白文件应视为不存在: %q", a.systemPrompt())
	}
}

func TestSystemPromptFrozen(t *testing.T) {
	a := newTestAgent(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeAgents(t, filepath.Join(cwd, "AGENTS.md"), "规则 v1")
	a.NewSession()
	before := a.systemPrompt()
	writeAgents(t, filepath.Join(cwd, "AGENTS.md"), "规则 v2")
	if a.systemPrompt() != before {
		t.Error("会话内快照应冻结")
	}
	a.NewSession()
	if !strings.Contains(a.systemPrompt(), "规则 v2") {
		t.Errorf("新会话应重读最新文件: %q", a.systemPrompt())
	}
}

func TestSaveLoadSystemSnapshot(t *testing.T) {
	a := newTestAgent(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeAgents(t, filepath.Join(cwd, "AGENTS.md"), "规则 v1")
	a.NewSession()
	a.history = []Message{
		{Role: "user", Content: "问题一"},
		{Role: "assistant", Content: "回答一"},
	}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSuffix(filepath.Base(a.store.path()), ".jsonl")

	writeAgents(t, filepath.Join(cwd, "AGENTS.md"), "规则 v2")
	b := newTestAgent(t)
	b.store.dir = a.store.dir
	if err := b.LoadSession(id); err != nil {
		t.Fatal(err)
	}
	if len(b.history) != 2 {
		t.Fatalf("载入历史数: %d", len(b.history))
	}
	if !strings.Contains(b.systemPrompt(), "规则 v1") {
		t.Errorf("应还原文件内快照而非重读: %q", b.systemPrompt())
	}

	b.history = append(b.history, Message{Role: "user", Content: "问题二"})
	if err := b.save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(b.store.path())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), `"role":"system"`); got != 1 {
		t.Errorf("system 行应仅 1 条: %d", got)
	}
	c := newTestAgent(t)
	c.store.dir = a.store.dir
	if err := c.LoadSession(id); err != nil {
		t.Fatal(err)
	}
	if len(c.history) != 3 {
		t.Errorf("追加保存后应为 3 条: %d", len(c.history))
	}
	if !strings.Contains(c.systemPrompt(), "规则 v1") {
		t.Errorf("二次载入快照: %q", c.systemPrompt())
	}
}

func TestLoadSessionLegacyFormat(t *testing.T) {
	a := newTestAgent(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeAgents(t, filepath.Join(cwd, "AGENTS.md"), "旧规则")
	legacy := filepath.Join(a.store.dir, "20260101-090000.jsonl")
	content := `{"role":"user","content":"历史问题"}` + "\n" + `{"role":"assistant","content":"历史回答"}` + "\n"
	if err := os.WriteFile(legacy, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadSession("20260101-090000"); err != nil {
		t.Fatal(err)
	}
	if len(a.history) != 2 {
		t.Fatalf("旧格式载入历史数: %d", len(a.history))
	}
	if !strings.Contains(a.systemPrompt(), "旧规则") {
		t.Errorf("旧格式应回退快照当前文件: %q", a.systemPrompt())
	}
}

func TestLegacySessionNotAppendSystem(t *testing.T) {
	a := newTestAgent(t)
	legacy := filepath.Join(a.store.dir, "20260101-090001.jsonl")
	content := `{"role":"user","content":"历史问题"}` + "\n"
	if err := os.WriteFile(legacy, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadSession("20260101-090001"); err != nil {
		t.Fatal(err)
	}
	a.history = append(a.history, Message{Role: "assistant", Content: "新回答"})
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"role":"system"`) {
		t.Fatalf("旧格式会话不应补写 system 行: %s", data)
	}
	if !strings.Contains(string(data), "新回答") {
		t.Fatalf("新消息应落盘: %s", data)
	}
}

func TestListSessionsSkipsSystemLine(t *testing.T) {
	a := newTestAgent(t)
	content := `{"role":"system","content":"sys"}` + "\n" +
		`{"role":"user","content":"标题问题"}` + "\n" +
		`{"role":"assistant","content":"好"}` + "\n"
	p := filepath.Join(a.store.dir, "20260101-100000.jsonl")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListSessions()
	if err != nil || len(list) != 1 {
		t.Fatalf("列表异常: %+v %v", list, err)
	}
	if list[0].Msgs != 2 {
		t.Errorf("system 行不应计入条数: %d", list[0].Msgs)
	}
	if list[0].Summary != "标题问题" {
		t.Errorf("标题应取首条 user: %q", list[0].Summary)
	}
}

func TestStatsFollowsLastRequestAndAccumulates(t *testing.T) {
	a := newTestAgent(t)
	a.stats.record(&Usage{PromptTokens: 1200, CacheHitTokens: 980})
	a.stats.record(&Usage{PromptTokens: 2000, CacheHitTokens: 600})
	st := a.Stats()
	if st.ContextTokens != 2000 || st.ContextHit != 600 {
		t.Errorf("上下文与单次命中量应取最近一次而非累计: %+v", st)
	}
	if st.CacheHitTokens != 1580 || st.PromptTokens != 3200 {
		t.Errorf("缓存与 prompt 应为累计: %+v", st)
	}
}

func TestSystemPromptIsInjectedBase(t *testing.T) {
	a := newTestAgent(t)
	if got := a.systemPrompt(); got != testBasePrompt {
		t.Errorf("system 应等于注入的基座（无 AGENTS.md）: %q", got)
	}
}

func TestEnvStableInSession(t *testing.T) {
	a := newTestAgent(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	before := a.systemPrompt()
	for _, f := range []string{"go.mod", "Makefile", "package.json"} {
		if err := os.WriteFile(filepath.Join(cwd, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if a.systemPrompt() != before {
		t.Errorf("会话期间 system 快照不得变更:\n got %q\nwant %q", a.systemPrompt(), before)
	}
}

func TestLegacyPromptFlag(t *testing.T) {
	assertLegacy := func(firstLine string) {
		t.Helper()
		a := newTestAgent(t)
		path := filepath.Join(a.store.dir, "20260101-080000.jsonl")
		content := firstLine + "\n" + `{"role":"user","content":"q"}` + "\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := a.LoadSession("20260101-080000"); err != nil {
			t.Fatal(err)
		}
		if got := a.LegacyPrompt(); got != true {
			t.Errorf("旧格式应识别 legacy: %v", got)
		}
	}
	assertLegacy(`{"role":"system","content":"规则\n\n## 运行环境\n\n- 系统: linux"}`)
	assertLegacy(`{"role":"system","content":"规则\n\n## 可用工具\n\n- run_shell"}`)
}

func TestLegacyPromptFlagNewFormat(t *testing.T) {
	a := newTestAgent(t)
	path := filepath.Join(a.store.dir, "20260101-080100.jsonl")
	content := `{"role":"system","content":"规则"}` + "\n" + `{"role":"user","content":"q"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadSession("20260101-080100"); err != nil {
		t.Fatal(err)
	}
	if a.LegacyPrompt() {
		t.Error("新格式不应标记 legacy")
	}
}

func TestTotalTokensCountsReasoningItems(t *testing.T) {
	isolatePromptEnv(t)
	a := newTestAgent(t)
	a.history = []Message{{Role: "assistant", Content: "ok"}}
	without := a.totalTokens()
	reasoning := strings.Repeat("推理内容", 400)
	a.history = []Message{{Role: "assistant", Content: "ok", ReasoningItems: []ReasoningItem{{ID: "r1", Content: reasoning}}}}
	with := a.totalTokens()
	if with-without != estimateTokens(reasoning) {
		t.Fatalf("推理未按 estimateTokens 计入: 差 %d, 期望 %d", with-without, estimateTokens(reasoning))
	}
	if with-without < 1000 {
		t.Fatalf("推理增量过小: %d", with-without)
	}
}

func TestTotalTokensCountsMultipleReasoningItems(t *testing.T) {
	isolatePromptEnv(t)
	a := newTestAgent(t)
	a.history = []Message{{Role: "assistant", ReasoningItems: []ReasoningItem{
		{ID: "r1", Content: strings.Repeat("a", 100)},
		{ID: "r2", Content: strings.Repeat("b", 100)},
	}}}
	multi := a.totalTokens()
	a.history = []Message{{Role: "assistant", ReasoningItems: []ReasoningItem{{ID: "r1", Content: strings.Repeat("a", 100)}}}}
	single := a.totalTokens()
	if multi-single != estimateTokens(strings.Repeat("b", 100)) {
		t.Fatalf("多条 reasoning 未全部计入: 差 %d", multi-single)
	}
}
