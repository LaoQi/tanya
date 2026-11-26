package repl

import (
	"context"
	"encoding/json"
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

// newHintREPL 按真实装配顺序造「钩子 → agent → REPL」：钩子随 agent.New 注册，REPL 绑渲染通道。
func newHintREPL(t *testing.T, tokens int, srv *httptest.Server, opts ...agent.Option) (*REPL, *HandoffHint, *syncBuf, *syncBuf) {
	return newHintREPLMode(t, tokens, srv, modeRich, ttyRich(), opts...)
}

func newHintREPLMode(t *testing.T, tokens int, srv *httptest.Server, mode outMode, prof term.Profile, opts ...agent.Option) (*REPL, *HandoffHint, *syncBuf, *syncBuf) {
	t.Helper()
	h := NewHandoffHint(tokens)
	opts = append(opts, agent.WithHooks(h))
	a := newNextAgent(t, srv.URL, opts...)
	out, errb := &syncBuf{}, &syncBuf{}
	r, err := NewREPL(a, "› ", WithStreams(NewStreams(out, errb, mode)), WithProfile(prof),
		WithToolViews(testViews()), WithConsole(newFakeTerm()), WithHandoffHint(h))
	if err != nil {
		t.Fatal(err)
	}
	return r, h, out, errb
}

func setHintTokens(t *testing.T, h *HandoffHint, n int) {
	t.Helper()
	h.mu.Lock()
	h.tokens = n
	h.mu.Unlock()
}

// warmHint 先跑一轮不触发提醒的对话，让上下文估算脱离 0（否则任何阈值都不成立）。
func warmHint(t *testing.T, r *REPL, h *HandoffHint) {
	t.Helper()
	setHintTokens(t, h, 0)
	r.ask("预热")
	setHintTokens(t, h, 1)
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

// 钩子注入的事实行落在用户输入之后（BeforeRequest 在 user 消息入 history 后、
// 构造请求之前），且本轮请求就带着它。
func TestHandoffHintInjectsOncePerSession(t *testing.T) {
	srv := newHintServer(t)
	r, h, out, errb := newHintREPL(t, 1, srv)
	warmHint(t, r, h)

	r.ask("第一轮")
	hist := r.agent.History()
	if len(hist) != 5 {
		t.Fatalf("应注入提示 + 用户输入 + 回答，共 5 条: %+v", hist)
	}
	if hist[2].Content != "第一轮" {
		t.Errorf("用户输入应排在注入项之前: %+v", hist[2])
	}
	if hist[3].Role != "user" || !strings.Contains(hist[3].Content, "next_session") {
		t.Errorf("注入项应排在用户输入之后: %+v", hist[3])
	}
	if hist[4].Role != "assistant" {
		t.Errorf("回答应在注入项之后: %+v", hist[4])
	}

	r.ask("第二轮")
	if hist := r.agent.History(); len(hist) != 7 {
		t.Fatalf("同会话不应重复注入: %+v", hist)
	}
	if got := strings.Count(term.Strip(out.String()), "已向模型注入交接建议"); got != 1 {
		t.Errorf("屏幕提示应只出现一次，实为 %d: %q", got, out.String())
	}
	if errb.String() != "" {
		t.Errorf("不应有错误输出: %q", errb.String())
	}
	if !h.fired() {
		t.Error("应记录本会话已提醒")
	}
}

func TestHandoffHintDisabled(t *testing.T) {
	srv := newHintServer(t)
	r, _, out, _ := newHintREPL(t, 0, srv)

	r.ask("干活")
	if hist := r.agent.History(); len(hist) != 2 {
		t.Errorf("阈值为 0 不应注入: %+v", hist)
	}
	if strings.Contains(term.Strip(out.String()), "交接建议") {
		t.Errorf("关闭时不应有屏幕提示: %q", out.String())
	}
}

func TestHandoffHintBelowThreshold(t *testing.T) {
	srv := newHintServer(t)
	r, _, _, _ := newHintREPL(t, 10*1000*1000, srv)

	r.ask("干活")
	if hist := r.agent.History(); len(hist) != 2 {
		t.Errorf("未达阈值不应注入: %+v", hist)
	}
}

func TestHandoffHintSkippedForNextCommand(t *testing.T) {
	srv := newHintServer(t)
	r, h, _, _ := newHintREPL(t, 1, srv)
	warmHint(t, r, h)

	r.nextPending = true
	r.runPrompt(agent.MsgHandoffPrompt, false)
	hist := r.agent.History()
	if len(hist) != 4 {
		t.Fatalf("/next 路径不应再注入提醒: %+v", hist)
	}
	for _, m := range hist {
		if strings.Contains(m.Content, "提醒阈值") {
			t.Fatalf("/next 路径不应再注入提醒: %+v", hist)
		}
	}
	if h.fired() {
		t.Error("/next 轮不应记为已提醒")
	}
}

func TestHandoffHintRepeatsAfterNewSession(t *testing.T) {
	srv := newHintServer(t)
	r, h, out, _ := newHintREPL(t, 1, srv)
	warmHint(t, r, h)

	r.ask("第一轮")
	if !h.fired() {
		t.Fatal("首轮应注入")
	}
	r.handleCommand("/new")
	if h.fired() {
		t.Error("/new 后应由 SessionEvent 重新武装")
	}
	r.ask("预热二")
	r.ask("第二轮")
	if got := strings.Count(term.Strip(out.String()), "已向模型注入交接建议"); got != 2 {
		t.Errorf("换会话后应可再提醒一次，实为 %d: %q", got, out.String())
	}
}

func TestHandoffHintNoSave(t *testing.T) {
	srv := newHintServer(t)
	r, h, _, _ := newHintREPL(t, 1, srv, agent.NoSave(true))
	warmHint(t, r, h)

	r.ask("干活")
	hist := r.agent.History()
	if len(hist) != 5 || !strings.Contains(hist[3].Content, "next_session") {
		t.Errorf("不落盘模式下仍应注入: %+v", hist)
	}
	if file := r.agent.SessionFile(); file != "" {
		t.Errorf("不落盘模式不应有会话文件: %q", file)
	}
}

func TestHandoffHintPlainStillPrints(t *testing.T) {
	srv := newHintServer(t)
	r, h, out, _ := newHintREPLMode(t, 1, srv, modePlain, nonTTYProf)
	warmHint(t, r, h)

	r.runPrompt("干活", false)
	hist := r.agent.History()
	if len(hist) != 5 || !strings.Contains(hist[3].Content, "next_session") {
		t.Errorf("plain 面仍应注入提示: %+v", hist)
	}
	if !strings.Contains(out.String(), "已向模型注入交接建议") {
		t.Errorf("plain 面提示行属命令反馈，应上屏: %q", out.String())
	}
}

// echoProbeTool 是工具轮的桩：让 BeforeRequest 在 tool 轮之后才可能达阈。
type echoProbeTool struct{}

func (echoProbeTool) Name() string { return "echo_probe" }

func (echoProbeTool) Definition() agent.ToolDef {
	return agent.NewToolDef("echo_probe", "测试桩：原样返回", `{"type":"object","properties":{}}`)
}

func (echoProbeTool) Invoke(context.Context, string) agent.ToolResult {
	return agent.ToolResult{Text: "echo ok"}
}

func sseUsage(w http.ResponseWriter, prompt, completion int) {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{},
		"usage":   map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion},
	})
	fmt.Fprintf(w, "data: %s\n\n", b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

type hintReqBody struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// 阈值在工具轮之后才达到时，提醒注入在 tool 结果之后、下一次请求之前——本轮请求即带上。
func TestHandoffHintInjectsInsideToolRound(t *testing.T) {
	var reqs []hintReqBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body hintReqBody
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reqs = append(reqs, body)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(reqs) == 1 {
			sseToolArgs(w, "call-1", "echo_probe", `{}`)
			sseUsage(w, 5000, 4)
		} else {
			sseChunk(w, map[string]any{"content": "好了"})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	r, h, out, errb := newHintREPL(t, 1000, srv, agent.WithTools(echoProbeTool{}))
	r.ask("干活")

	if len(reqs) != 2 {
		t.Fatalf("请求数 = %d", len(reqs))
	}
	hist := r.agent.History()
	if len(hist) != 5 {
		t.Fatalf("history 应为 输入+工具调用+结果+注入+回答: %+v", hist)
	}
	if hist[1].Role != "assistant" || len(hist[1].ToolCalls) == 0 {
		t.Errorf("第 2 条应为带 tool_calls 的 assistant: %+v", hist[1])
	}
	if hist[2].Role != "tool" {
		t.Errorf("第 3 条应为工具结果: %+v", hist[2])
	}
	if hist[3].Role != "user" || !strings.Contains(hist[3].Content, "next_session") {
		t.Errorf("注入项应落在 tool 结果之后: %+v", hist[3])
	}
	if hist[4].Role != "assistant" {
		t.Errorf("末条应为回答: %+v", hist[4])
	}
	// 第二次请求已带上注入行（本轮生效，不额外发请求）。
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "next_session") {
		t.Errorf("第二次请求末尾应为注入行, got %+v", last)
	}
	if !h.fired() {
		t.Error("应记为已提醒")
	}
	if got := strings.Count(term.Strip(out.String()), "已向模型注入交接建议"); got != 1 {
		t.Errorf("屏幕提示应只出现一次，实为 %d: %q", got, out.String())
	}
	if errb.String() != "" {
		t.Errorf("不应有错误输出: %q", errb.String())
	}
}

// 没有 REPL 回合在驱动（未绑定渲染通道）时不注入：单发 ask 与库外直接调 Ask 的路径不参与。
func TestHandoffHintInactiveOutsideREPLTurn(t *testing.T) {
	srv := newHintServer(t)
	h := NewHandoffHint(1)
	a := newNextAgent(t, srv.URL, agent.WithHooks(h))
	if err := a.Ask(context.Background(), "干活", nil); err != nil {
		t.Fatal(err)
	}
	if hist := a.History(); len(hist) != 2 {
		t.Errorf("未经 REPL 回合不应注入: %+v", hist)
	}
	if h.fired() {
		t.Error("不应记为已提醒")
	}
}
