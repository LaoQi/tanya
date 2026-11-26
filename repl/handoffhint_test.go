package repl

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LaoQi/battlelab/agent"
	"github.com/LaoQi/tanya/render/term"
)

func newHintServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sseChunk(w, map[string]any{"content": "好"})
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// warmHint 先跑一轮不触发提醒的对话，让上下文估算脱离 0（否则任何阈值都不成立）。
func warmHint(t *testing.T, r *REPL) {
	t.Helper()
	r.hintTokens = 0
	r.ask("预热")
	r.hintTokens = 1
}

func TestContextUsedPicksLarger(t *testing.T) {
	cases := []struct{ api, est, want int }{
		{0, 500, 500},
		{800, 500, 800},
		{500, 500, 500},
		{0, 0, 0},
	}
	for _, c := range cases {
		got := contextUsed(agent.Stats{ContextTokens: c.api, Est: c.est})
		if got != c.want {
			t.Errorf("api=%d est=%d: 应为 %d，实为 %d", c.api, c.est, c.want, got)
		}
	}
}

func TestHandoffHintInjectsOncePerSession(t *testing.T) {
	srv := newHintServer(t)
	a := newNextAgent(t, srv.URL)
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	warmHint(t, r)

	r.ask("第一轮")
	h := a.History()
	if len(h) != 5 {
		t.Fatalf("应注入提示 + 用户输入 + 回答，共 5 条: %+v", h)
	}
	if h[2].Role != "user" || !strings.Contains(h[2].Content, "next_session") {
		t.Errorf("注入项应排在用户输入之前: %+v", h[2])
	}
	if h[3].Content != "第一轮" || h[4].Role != "assistant" {
		t.Errorf("用户输入与回答应紧随提示之后: %+v", h[3:])
	}

	r.ask("第二轮")
	if h := a.History(); len(h) != 7 {
		t.Fatalf("同会话不应重复注入: %+v", h)
	}
	if got := strings.Count(term.Strip(out.String()), "已向模型注入交接建议"); got != 1 {
		t.Errorf("屏幕提示应只出现一次，实为 %d: %q", got, out.String())
	}
	if errb.String() != "" {
		t.Errorf("不应有错误输出: %q", errb.String())
	}
	if !r.hintDone {
		t.Error("应记录本会话已提醒")
	}
}

func TestHandoffHintDisabled(t *testing.T) {
	srv := newHintServer(t)
	a := newNextAgent(t, srv.URL)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.hintTokens = 0

	r.ask("干活")
	if h := a.History(); len(h) != 2 {
		t.Errorf("阈值为 0 不应注入: %+v", h)
	}
	if strings.Contains(term.Strip(out.String()), "交接建议") {
		t.Errorf("关闭时不应有屏幕提示: %q", out.String())
	}
}

func TestHandoffHintBelowThreshold(t *testing.T) {
	srv := newHintServer(t)
	a := newNextAgent(t, srv.URL)
	r, _, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.hintTokens = 10 * 1000 * 1000

	r.ask("干活")
	if h := a.History(); len(h) != 2 {
		t.Errorf("未达阈值不应注入: %+v", h)
	}
}

func TestHandoffHintSkippedForNextCommand(t *testing.T) {
	srv := newHintServer(t)
	a := newNextAgent(t, srv.URL)
	r, _, _ := newTestREPLAgent(t, a, newFakeTerm())
	warmHint(t, r)

	r.nextPending = true
	r.runPrompt(agent.MsgHandoffPrompt, false)
	if h := a.History(); len(h) != 4 || strings.Contains(h[2].Content, "交接进度") {
		t.Errorf("/next 路径不应再注入提醒: %+v", h)
	}
}
func TestHandoffHintRepeatsAfterNewSession(t *testing.T) {
	srv := newHintServer(t)
	a := newNextAgent(t, srv.URL)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	warmHint(t, r)

	r.ask("第一轮")
	if !r.hintDone {
		t.Fatal("首轮应注入")
	}
	r.handleCommand("/new")
	if r.hintDone {
		t.Error("/new 后应重新武装")
	}
	r.ask("预热二")
	r.ask("第二轮")
	if got := strings.Count(term.Strip(out.String()), "已向模型注入交接建议"); got != 2 {
		t.Errorf("换会话后应可再提醒一次，实为 %d: %q", got, out.String())
	}
}

func TestHandoffHintNoSave(t *testing.T) {
	srv := newHintServer(t)
	a := newNextAgent(t, srv.URL, agent.NoSave(true))
	r, _, _ := newTestREPLAgent(t, a, newFakeTerm())
	warmHint(t, r)

	r.ask("干活")
	h := a.History()
	if len(h) != 5 || !strings.Contains(h[2].Content, "next_session") {
		t.Errorf("不落盘模式下仍应注入: %+v", h)
	}
	if file := a.SessionFile(); file != "" {
		t.Errorf("不落盘模式不应有会话文件: %q", file)
	}
}

func TestHandoffHintPlainStillPrints(t *testing.T) {
	srv := newHintServer(t)
	a := newNextAgent(t, srv.URL)
	r, out, _ := newTestREPLMode(t, nil, modePlain, nonTTYProf)
	r.agent = a
	warmHint(t, r)

	r.runPrompt("干活", false)
	h := a.History()
	if len(h) != 5 || !strings.Contains(h[2].Content, "next_session") {
		t.Errorf("plain 面仍应注入提示: %+v", h)
	}
	if !strings.Contains(out.String(), "已向模型注入交接建议") {
		t.Errorf("plain 面提示行属命令反馈，应上屏: %q", out.String())
	}
}
