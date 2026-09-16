package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
)

func newFarewellREPL(t *testing.T, a *agent.Agent, mode outMode) (*REPL, *syncBuf) {
	t.Helper()
	out := &syncBuf{}
	st := NewStreams(out, &syncBuf{}, mode)
	r, err := NewREPL(a, "› ", WithStreams(st), WithTerminal(newFakeTerm(), false))
	if err != nil {
		t.Fatal(err)
	}
	return r, out
}

func TestFarewellTextFull(t *testing.T) {
	file := filepath.Join(t.TempDir(), "20260916-153000.jsonl")
	got := farewellText(farewellInfo{
		session:  "20260916-153000",
		duration: 12*time.Minute + 3*time.Second,
		stats:    agent.Stats{Messages: 14, PromptTokens: 40100, CompletionTokens: 5100, TotalTokens: 45200, CacheHitTokens: 33000},
		file:     file,
	})
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("应为三行: %q", got)
	}
	if !strings.HasPrefix(lines[0], "会话 20260916-153000 · 时长 12m03s · 消息 14 条") {
		t.Errorf("首行: %q", lines[0])
	}
	if lines[1] != "用量 45.2k（prompt 40.1k / completion 5.1k）· 缓存 82.29%" {
		t.Errorf("用量行: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "会话文件 ") || !strings.HasSuffix(lines[2], file) {
		t.Errorf("文件行应为可用的完整路径（不缩写中间目录）: %q", lines[2])
	}
}

func TestFarewellTextNoUsageNoFile(t *testing.T) {
	got := farewellText(farewellInfo{session: "s1", duration: 900 * time.Millisecond})
	want := "会话 s1 · 时长 900ms · 消息 0 条\n用量 无（未收到 API usage）\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFarewellTextNoSave(t *testing.T) {
	got := farewellText(farewellInfo{duration: 3 * time.Second, noSave: true})
	want := "时长 3.0s · 消息 0 条\n用量 无（未收到 API usage）\n" + MsgFarewellNoFile + "\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFarewellTextNoFileWhenNotSaved(t *testing.T) {
	got := farewellText(farewellInfo{session: "s1", duration: time.Second})
	if strings.Contains(got, "会话文件") {
		t.Errorf("未落盘且可写时不应有文件行: %q", got)
	}
}

func TestFarewellRichOutput(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	st := a.Stats()
	if err := os.WriteFile(st.Session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, out := newFarewellREPL(t, a, modeRich)
	r.started = time.Now().Add(-90 * time.Second)
	r.farewell()
	got := out.String()
	if !strings.Contains(got, "会话 "+a.SessionID()) {
		t.Errorf("应含会话 id: %q", got)
	}
	if !strings.Contains(got, "时长 1m30s") {
		t.Errorf("应含运行时长: %q", got)
	}
	if !strings.Contains(got, "会话文件 "+st.Session) {
		t.Errorf("应含已落盘文件的完整路径: %q", got)
	}
}

func TestFarewellNoSaveREPL(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir(), agent.NoSave(true))
	r, out := newFarewellREPL(t, a, modeRich)
	r.farewell()
	got := out.String()
	if a.SessionID() != "" || a.SessionFile() != "" {
		t.Fatalf("只读模式不应有会话 id 与文件: %q %q", a.SessionID(), a.SessionFile())
	}
	if !strings.Contains(got, MsgFarewellNoFile) {
		t.Errorf("只读模式应提示未写入: %q", got)
	}
	if strings.Contains(got, ".jsonl") {
		t.Errorf("只读模式不应有路径行: %q", got)
	}
}

func TestFarewellSilentInPlain(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	for _, mode := range []outMode{modePlain, modePlainVerbose} {
		r, out := newFarewellREPL(t, a, mode)
		r.farewell()
		if out.String() != "" {
			t.Errorf("plain 档退出不应有收尾输出: %q", out.String())
		}
	}
}

func TestFarewellNilAgent(t *testing.T) {
	r, out, _ := newTestREPL(t, newFakeTerm())
	r.farewell()
	if out.String() != "" {
		t.Errorf("无 agent 时不应有输出: %q", out.String())
	}
}

func TestHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("无 HOME 可测")
	}
	p := filepath.Join(home, "proj", ".tanya", "sessions", "s.jsonl")
	if got, want := homePath(p), filepath.Join("~", "proj", ".tanya", "sessions", "s.jsonl"); got != want {
		t.Errorf("home 前缀应替换为 ~: got %q want %q", got, want)
	}
	if got := homePath("/etc/hosts"); got != "/etc/hosts" {
		t.Errorf("非 home 路径应原样: %q", got)
	}
}
