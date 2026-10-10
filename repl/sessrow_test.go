package repl

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/render/term"

	"github.com/LaoQi/tanya/agent"
)

func newSessTestAgent(t *testing.T, dir string, opts ...agent.Option) *agent.Agent {
	t.Helper()
	cfg := &agent.Config{
		BaseURL:     "http://127.0.0.1:1",
		Model:       "test-model",
		UserAgent:   agent.UserAgent("dev"),
		ConfigPath:  "/tmp/tanya-test-config.yaml",
		ApiProtocol: "chat",
		DataDir:     dir,
		SessionMode: "global",

		ArchiveThreshold: agent.DefaultArchiveThreshold,
		ArchiveKeep:      agent.DefaultArchiveKeep,
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
	pickByNumber(list, NewStreams(out, &syncBuf{}, modeRich).out, 0)
	want := "输入序号选择会话（回车取消）:\n  1   20260101-100000  " + list[0].ModTime.Format("01-02 15:04") + "    2条  第一条\n序号: "
	if out.String() != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestHandleCommandExit(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())

	if !r.handleCommand("/exit") {
		t.Error("/exit 应返回 true 表示退出")
	}
	if out.String() != "" {
		t.Errorf("退出输出由 Run 统一收尾，handleCommand 不应写字节: %q", out.String())
	}
}

func seedIntoSessionDir(t *testing.T, root, name, content string) string {
	t.Helper()
	sub := ""
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "sessions" {
			sub = p
			return fs.SkipAll
		}
		return nil
	})
	if sub == "" {
		t.Fatal("session 子目录不存在")
	}
	path := filepath.Join(sub, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPickByNumberColumnWidth 锁住序号菜单的按列截断：窄终端下每行不超 cols-1、不折行；
// 宽度不可知（cols=0）时保持原样输出，不按 rune 猜。
func TestPickByNumberColumnWidth(t *testing.T) {
	dir := t.TempDir()
	seed := `{"role":"user","content":"` + strings.Repeat("长", 200) + `"}` + "\n"
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
	pickByNumber(list, NewStreams(out, &syncBuf{}, modeRich).out, 40)
	for _, l := range strings.Split(out.String(), "\n") {
		if w := term.Width(l); w > 39 {
			t.Errorf("菜单行超宽 %d 列: %q", w, l)
		}
	}
	wide := &syncBuf{}
	pickByNumber(list, NewStreams(wide, &syncBuf{}, modeRich).out, 0)
	maxw := 0
	for _, l := range strings.Split(wide.String(), "\n") {
		if w := term.Width(l); w > maxw {
			maxw = w
		}
	}
	if maxw <= 39 {
		t.Errorf("宽度不可知时不应按终端列截断，最长行只有 %d 列", maxw)
	}
}
