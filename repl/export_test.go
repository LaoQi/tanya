package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleExportUsage(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/export")
	if !strings.Contains(out.String(), "用法: /export") {
		t.Errorf("无参应给用法: %q", out.String())
	}
	if errb.String() != "" {
		t.Errorf("无参不应报错: %q", errb.String())
	}
}

func TestHandleExportTooManyArgs(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, _, errb := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/export a.jsonl b.jsonl")
	if !strings.Contains(errb.String(), "参数过多") {
		t.Errorf("多参应报错: %q", errb.String())
	}
}

func TestHandleExportAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	target := filepath.Join(t.TempDir(), "recover.jsonl")
	r.handleCommand("/export " + target)
	if errb.String() != "" {
		t.Fatalf("导出失败: %q", errb.String())
	}
	if !strings.Contains(out.String(), "已导出") {
		t.Errorf("应报告导出结果: %q", out.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("目标文件未生成: %v", err)
	}
}

func TestHandleExportRelativePathUnderWorkspace(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, _, errb := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/export recover.jsonl")
	if errb.String() != "" {
		t.Fatalf("导出失败: %q", errb.String())
	}
	want := filepath.Join(a.Workspace(), "recover.jsonl")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("相对路径应落在工作区 %s: %v", want, err)
	}
}

func TestExportListedInCompletions(t *testing.T) {
	found := false
	for _, c := range slashCommands {
		if c == "/export" {
			found = true
		}
	}
	if !found {
		t.Error("/export 应登记在 slashCommands")
	}
	if !strings.Contains(helpText, "/export") {
		t.Error("helpText 应列出 /export")
	}
}

// 端到端演练：模拟事故——会话文件被删（sessions 目录也在），进程内存仍完整，
// /export 把内存历史救出，产物可被 /load 读回。
func TestExportRescuesAfterSessionFileDeleted(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	a.AppendNotice("第一条历史")
	a.AppendNotice("第二条历史")
	a.AppendNotice("第三条历史")
	sessFile := a.SessionFile()
	if sessFile == "" {
		t.Fatal("应有会话文件")
	}

	// 模拟事故：整树删除（含 sessions 目录）
	os.RemoveAll(filepath.Dir(sessFile))
	if _, err := os.Stat(sessFile); !os.IsNotExist(err) {
		t.Fatal("会话文件应已消失")
	}

	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	target := filepath.Join(dir, "rescue.jsonl")
	r.handleCommand("/export " + target)
	if errb.String() != "" {
		t.Fatalf("导出失败: %q", errb.String())
	}
	if !strings.Contains(out.String(), "已导出 3 条消息") {
		t.Fatalf("应导出 3 条: %q", out.String())
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"第一条历史", "第二条历史", "第三条历史"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("导出内容缺少 %q:\n%s", want, b)
		}
	}
}
