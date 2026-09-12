package repl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanyan/agent"
)

func newSessTestAgent(t *testing.T, dir string, opts ...agent.Option) *agent.Agent {
	t.Helper()
	cfg := &agent.Config{
		BaseURL:         "http://127.0.0.1:1",
		Model:           "test-model",
		UserAgent:       agent.DefaultUserAgent,
		GlobalSession:   dir,
		SessionMode:     "global",
		ToolOutputLines: 20,
	}
	a, err := agent.New(cfg, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSessionsRowFormat(t *testing.T) {
	m := time.Date(2026, 9, 7, 13, 56, 0, 0, time.Local)
	row := fmt.Sprintf(SessRow+"\n", "", "20260907-135638", m.Format("01-02 15:04"), 3, "你好")
	want := "20260907-135638  09-07 13:56    3条  你好\n"
	if row != want {
		t.Errorf("got %q want %q", row, want)
	}
}

func TestSessRowVerbCount(t *testing.T) {
	if n := strings.Count(SessRow, "%s"); n != 4 || !strings.Contains(SessRow, "%3d") {
		t.Errorf("SessRow 应为 4×%%s 加 %%3d 的五动词格式: %q", SessRow)
	}
}

func TestPickByNumberOutput(t *testing.T) {
	dir := t.TempDir()
	seed := `{"role":"user","content":"第一条"}` + "\n" + `{"role":"assistant","content":"答"}` + "\n"
	a := newSessTestAgent(t, dir)
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", seed)
	list, err := a.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("应扫描到 1 个会话: %d", len(list))
	}
	out := &syncBuf{}
	pickByNumber(list, NewStreams(out, &syncBuf{}, modeRich).out)
	want := "输入序号选择会话（回车取消）:\n  1   20260101-100000  " + list[0].ModTime.Format("01-02 15:04") + "    2条  第一条\n序号: "
	if out.String() != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestHandleCommandExit(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())

	exit := r.handleCommand("/exit")
	if !exit || out.String() != "再见\n" {
		t.Errorf("exit: ret=%v out=%q want %q", exit, out.String(), "再见\n")
	}
}

func seedIntoSessionDir(t *testing.T, root, name, content string) {
	t.Helper()
	var sub string
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			sub = filepath.Join(root, e.Name())
			break
		}
	}
	if sub == "" {
		t.Fatal("session 子目录不存在")
	}
	if err := os.WriteFile(filepath.Join(sub, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
