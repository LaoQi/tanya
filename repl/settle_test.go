package repl

import (
	"github.com/LaoQi/tanya/render/term"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
)

func withPlainProfile(t *testing.T) {
	t.Helper()
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: false, Colors: term.LevelNone})
	t.Cleanup(func() { term.SetProfile(old) })
}

func newSettleAgent(t *testing.T) *agent.Agent {
	t.Helper()
	cfg := &agent.Config{
		BaseURL:         "http://127.0.0.1:1",
		Model:           "test-model",
		UserAgent:       agent.DefaultUserAgent,
		ConfigPath:      "/tmp/tanya-test-config.yaml",
		ApiProtocol:     "chat",
		DataDir:         t.TempDir(),
		SessionMode:     "global",

		ArchiveThreshold: agent.DefaultArchiveThreshold,
		ArchiveKeep:      agent.DefaultArchiveKeep,
	}
	a, err := agent.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func infoWithUsage() agent.ResponseInfo {
	return agent.ResponseInfo{
		Duration:   1200 * time.Millisecond,
		FirstEvent: 300 * time.Millisecond,
		Usage:      &agent.Usage{PromptTokens: 1500, CompletionTokens: 10, TotalTokens: 1510},
	}
}

func TestResponseInfoFollowsSettledTail(t *testing.T) {
	withPlainProfile(t)
	a := newSettleAgent(t)
	r, buf, _ := newTestREPLAgent(t, a, newFakeTerm())
	turn := r.beginTurn(nil)
	turn.f.md.Write("计算结果是 42")

	turn.Handle(agent.Event{Kind: agent.EventResponse, Response: infoWithUsage()})
	out := buf.String()
	if !strings.Contains(out, "计算结果是 42") {
		t.Fatalf("滞留尾行应在回合结束结算输出，修复前会丢失直到 Ask 结束: %q", out)
	}
	if !strings.Contains(out, "↳") {
		t.Fatalf("应输出状态行: %q", out)
	}
	if strings.Index(out, "计算结果是 42") > strings.Index(out, "↳") {
		t.Errorf("滞留尾行应在状态行之前输出，不得把状态行插入渲染文本中间: %q", out)
	}
}

func TestToolStartFollowsSettledTail(t *testing.T) {
	withPlainProfile(t)
	a := newSettleAgent(t)
	r, buf, _ := newTestREPLAgent(t, a, newFakeTerm())
	turn := r.beginTurn(nil)
	turn.f.md.Write("准备运行计算")

	turn.Handle(agent.Event{Kind: agent.EventResponse, Response: infoWithUsage()})
	turn.Handle(agent.Event{Kind: agent.EventToolStart, ToolName: "calc", ToolArgs: `{"expression":"6*7"}`})
	out := buf.String()
	iTail := strings.Index(out, "准备运行计算")
	iStat := strings.Index(out, "↳")
	iTool := strings.Index(out, "▸ calc")
	if iTail < 0 || iStat < 0 || iTool < 0 {
		t.Fatalf("应依次出现 尾行/状态行/工具标题: %q", out)
	}
	if !(iTail < iStat && iStat < iTool) {
		t.Errorf("顺序应为 渲染尾行→状态行→工具标题，滞留尾行不得延迟到工具块之后: %q", out)
	}
}
