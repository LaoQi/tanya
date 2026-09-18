package repl

import (
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/render/term"
)

func newAutoArchiveAgent(t *testing.T, dir string, threshold, keep int) *agent.Agent {
	t.Helper()
	cfg := &agent.Config{
		BaseURL:          "http://127.0.0.1:1",
		Model:            "test-model",
		UserAgent:        agent.DefaultUserAgent,
		DataDir:          dir,
		SessionMode:      "global",
		ToolOutputLines:  20,
		AutoArchive:      true,
		ArchiveThreshold: threshold,
		ArchiveKeep:      keep,
	}
	a, err := agent.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func seedAutoArchiveSessions(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		seedOldSession(t, dir, "202601"+string(rune('a'+i))+"-000000", 40*24*time.Hour)
	}
}

func countSessions(t *testing.T, a *agent.Agent) (active, archived int) {
	t.Helper()
	list, err := a.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, si := range list {
		if si.Archived {
			archived++
		} else {
			active++
		}
	}
	return active, archived
}

func runAutoArchivePrompt(t *testing.T, a *agent.Agent, keys ...readline.KeyEvent) (*syncBuf, *syncBuf) {
	t.Helper()
	ttyProfile(t, term.Profile{TTY: true, Colors: term.LevelNone})
	out, errb := &syncBuf{}, &syncBuf{}
	r, err := NewREPL(a, "› ", WithStreams(NewStreams(out, errb, modeRich)), WithTerminal(newFakeTerm(keys...), true))
	if err != nil {
		t.Fatal(err)
	}
	r.autoArchivePrompt()
	return out, errb
}

func TestAutoArchivePromptAccept(t *testing.T) {
	for _, answer := range []string{"y", "yes", "YES", " Y "} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			dir := t.TempDir()
			a := newAutoArchiveAgent(t, dir, 4, 2)
			seedAutoArchiveSessions(t, dir, 4)

			sug, ok := a.SuggestArchive()
			if !ok || sug.Active != 4 || sug.Candidates != 2 {
				t.Fatalf("建议异常: %+v ok=%v", sug, ok)
			}
			out, errb := runAutoArchivePrompt(t, a, typed(answer)...)

			got := out.String()
			if !strings.Contains(got, "当前活跃会话 4 个") || !strings.Contains(got, "将归档 2 个（约 ") || !strings.Contains(got, "保留最近 2 个") {
				t.Errorf("提示文案异常: %q", got)
			}
			if !strings.Contains(got, "已归档 2 个会话") {
				t.Errorf("归档报告异常: %q", got)
			}
			if errb.String() != "" {
				t.Errorf("不应有错误输出: %q", errb.String())
			}
			if active, archived := countSessions(t, a); active != 2 || archived != 2 {
				t.Errorf("归档后应保留 2 个活跃: active=%d archived=%d", active, archived)
			}
		})
	}
}

func TestAutoArchivePromptDecline(t *testing.T) {
	for _, answer := range []string{"n", "", "no", "随便"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			dir := t.TempDir()
			a := newAutoArchiveAgent(t, dir, 4, 2)
			seedAutoArchiveSessions(t, dir, 4)
			out, errb := runAutoArchivePrompt(t, a, typed(answer)...)

			got := out.String()
			if !strings.Contains(got, MsgArchiveCancel) {
				t.Errorf("应提示已取消: %q", got)
			}
			if strings.Contains(got, "已归档") {
				t.Errorf("拒绝后不应归档: %q", got)
			}
			if errb.String() != "" {
				t.Errorf("拒绝不应报错: %q", errb.String())
			}
			if active, archived := countSessions(t, a); active != 4 || archived != 0 {
				t.Errorf("拒绝后应原样保留: active=%d archived=%d", active, archived)
			}
		})
	}
}

func TestAutoArchivePromptBelowThresholdSkips(t *testing.T) {
	dir := t.TempDir()
	a := newAutoArchiveAgent(t, dir, 8, 2)
	seedAutoArchiveSessions(t, dir, 4)
	out, errb := runAutoArchivePrompt(t, a, typed("y")...)
	if out.String() != "" || errb.String() != "" {
		t.Errorf("未达阈值不应询问: %q %q", out.String(), errb.String())
	}
	if active, archived := countSessions(t, a); active != 4 || archived != 0 {
		t.Errorf("未达阈值不应归档: active=%d archived=%d", active, archived)
	}
}

func TestAutoArchivePromptPlainSkips(t *testing.T) {
	dir := t.TempDir()
	a := newAutoArchiveAgent(t, dir, 4, 2)
	seedAutoArchiveSessions(t, dir, 4)
	out, errb := &syncBuf{}, &syncBuf{}
	r, err := NewREPL(a, "› ", WithStreams(NewStreams(out, errb, modePlain)), WithTerminal(newFakeTerm(), false))
	if err != nil {
		t.Fatal(err)
	}
	r.autoArchivePrompt()
	if out.String() != "" || errb.String() != "" {
		t.Errorf("纯文本模式不应询问: %q %q", out.String(), errb.String())
	}
	if active, archived := countSessions(t, a); active != 4 || archived != 0 {
		t.Errorf("纯文本模式不应归档: active=%d archived=%d", active, archived)
	}
}

func TestAutoArchivePromptNonTTYSkips(t *testing.T) {
	dir := t.TempDir()
	a := newAutoArchiveAgent(t, dir, 4, 2)
	seedAutoArchiveSessions(t, dir, 4)
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	r.autoArchivePrompt()
	if out.String() != "" || errb.String() != "" {
		t.Errorf("stdout 非终端不应询问: %q %q", out.String(), errb.String())
	}
	if active, archived := countSessions(t, a); active != 4 || archived != 0 {
		t.Errorf("stdout 非终端不应归档: active=%d archived=%d", active, archived)
	}
}
