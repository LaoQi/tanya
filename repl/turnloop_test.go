package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/render/term"
)

func sseChunk(w http.ResponseWriter, delta map[string]any) {
	b, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": delta}}})
	fmt.Fprintf(w, "data: %s\n\n", b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sseToolCall(w http.ResponseWriter, id, name string) {
	b, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{
		"tool_calls": []map[string]any{{
			"index": 0, "id": id, "type": "function",
			"function": map[string]any{"name": name, "arguments": ""},
		}},
	}}}})
	fmt.Fprintf(w, "data: %s\n\n", b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func newAskAgent(t *testing.T, baseURL string, tools ...agent.Tool) *agent.Agent {
	t.Helper()
	cfg := &agent.Config{
		BaseURL:          baseURL,
		APIKey:           "test-key",
		Model:            "test-model",
		UserAgent:        agent.UserAgent("dev"),
		ConfigPath:       "/tmp/tanya-test-config.yaml",
		ApiProtocol:      "chat",
		DataDir:          t.TempDir(),
		SessionMode:      "global",
		ArchiveThreshold: agent.DefaultArchiveThreshold,
		ArchiveKeep:      agent.DefaultArchiveKeep,
	}
	opts := []agent.Option{agent.NoSave(true)}
	for _, tl := range tools {
		opts = append(opts, agent.WithTools(tl))
	}
	a, err := agent.New(cfg, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestTurnHotkeyReplayFromOff(t *testing.T) {
	r, out, _ := newTestREPL(t, newFakeTerm())
	r.showReasoning = false
	tn := r.beginTurn(func() {})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "隐藏的思考"})
	if got := term.Strip(out.String()); strings.Contains(got, "隐藏的思考") {
		t.Fatalf("关闭期思维链不应上屏: %q", got)
	}
	tn.Hotkey(readline.KeyEvent{Code: readline.KeyCtrlO})
	got := term.Strip(out.String())
	if !strings.Contains(got, "思考") || !strings.Contains(got, "隐藏的思考") {
		t.Errorf("开档应重放整段: %q", got)
	}
	out.Reset()
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "重放后的追加\n"})
	if got := term.Strip(out.String()); !strings.Contains(got, "重放后的追加") {
		t.Errorf("重放后新 delta 应继续上屏: %q", got)
	}
	tn.Hotkey(readline.KeyEvent{Code: readline.KeyCtrlO})
	if got := term.Strip(out.String()); !strings.Contains(got, "思考结束") {
		t.Errorf("再关应收尾: %q", got)
	}
	tn.End(nil)
}

func TestTurnHotkeyEmptySegNotice(t *testing.T) {
	r, out, _ := newTestREPL(t, newFakeTerm())
	r.showReasoning = false
	tn := r.beginTurn(func() {})
	tn.Hotkey(readline.KeyEvent{Code: readline.KeyCtrlO})
	if got := term.Strip(out.String()); !strings.Contains(got, "下一段生效") {
		t.Errorf("空段开档应提示下一段生效: %q", got)
	}
	tn.Hotkey(readline.KeyEvent{Code: readline.KeyCtrlO})
	if got := term.Strip(out.String()); !strings.Contains(got, MsgReasoningOff) {
		t.Errorf("空段关档应提示已关: %q", got)
	}
	tn.End(nil)
}

func TestTurnSegResetOnBoundary(t *testing.T) {
	r, out, _ := newTestREPL(t, newFakeTerm())
	r.showReasoning = false
	tn := r.beginTurn(func() {})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "第一段"})
	tn.Handle(agent.Event{Kind: agent.EventContent, Text: "正文"})
	out.Reset()
	tn.Hotkey(readline.KeyEvent{Code: readline.KeyCtrlO})
	got := term.Strip(out.String())
	if strings.Contains(got, "第一段") {
		t.Errorf("段边界后旧段不应重放: %q", got)
	}
	if !strings.Contains(got, "下一段生效") {
		t.Errorf("新段未开始应提示: %q", got)
	}
	tn.End(nil)
}

func TestTurnHotkeyClosesOpenBlock(t *testing.T) {
	r, out, _ := newTestREPL(t, newFakeTerm())
	r.showReasoning = true
	tn := r.beginTurn(func() {})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "进行中的思考"})
	out.Reset()
	tn.Hotkey(readline.KeyEvent{Code: readline.KeyCtrlO})
	got := term.Strip(out.String())
	if !strings.Contains(got, "思考结束") {
		t.Fatalf("显示中关档应收尾: %q", got)
	}
	out.Reset()
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "追加"})
	if got := term.Strip(out.String()); strings.Contains(got, "追加") {
		t.Errorf("关档后思维链不应上屏: %q", got)
	}
	tn.End(nil)
}

func TestTurnHotkeyOnWithoutDelta(t *testing.T) {
	r, out, _ := newTestREPL(t, newFakeTerm())
	r.showReasoning = true
	tn := r.beginTurn(func() {})
	tn.Hotkey(readline.KeyEvent{Code: readline.KeyCtrlO})
	got := term.Strip(out.String())
	if strings.Contains(got, "思考结束") {
		t.Errorf("无内容关档不应收尾: %q", got)
	}
	if !strings.Contains(got, MsgReasoningOff) {
		t.Errorf("应提示已关: %q", got)
	}
	tn.End(nil)
}

func TestRunTurnCtrlOStream(t *testing.T) {
	dev := newFakeTerm()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		time.Sleep(250 * time.Millisecond)
		sseChunk(w, map[string]any{"reasoning_content": "落定的思考"})
		dev.pushCtrlO()
		sseChunk(w, map[string]any{"content": "答案"})
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	a := newAskAgent(t, srv.URL)
	r, out, _ := newTestREPLAgent(t, a, dev)
	r.showReasoning = false
	r.ask("你好")
	got := term.Strip(out.String())
	if !strings.Contains(got, "落定的思考") || !strings.Contains(got, "答案") {
		t.Errorf("Ctrl+O 后思维链应可见且正文完整: %q", got)
	}
	if !strings.Contains(got, "─── 思考") {
		t.Errorf("应出现思维链分隔: %q", got)
	}
}

func TestRunTurnToolBlockRenderedBeforeInvoke(t *testing.T) {
	dev := newFakeTerm()
	var out *syncBuf
	var rendered atomic.Bool
	probe := agent.NewTool("probe", "记录执行时刻", `{"type":"object"}`,
		func(ctx context.Context, args string) agent.ToolResult {
			rendered.Store(out != nil && strings.Contains(term.Strip(out.String()), "probe"))
			return agent.ToolResult{Text: "ok"}
		})
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if n.Add(1) == 1 {
			time.Sleep(150 * time.Millisecond)
			sseChunk(w, map[string]any{"reasoning_content": "工具前思考"})
			sseToolCall(w, "call-1", "probe")
		} else {
			sseChunk(w, map[string]any{"content": "收尾"})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	a := newAskAgent(t, srv.URL, probe)
	r, ob, _ := newTestREPLAgent(t, a, dev)
	out = ob
	out.onWrite = func(p []byte) {
		if bytes.Contains(p, []byte("probe")) {
			time.Sleep(120 * time.Millisecond)
		}
	}
	r.showReasoning = true
	r.ask("跑工具")
	if !rendered.Load() {
		t.Error("工具块应已先于工具执行上屏（ToolStart ack 握手）")
	}
	if got := term.Strip(out.String()); !strings.Contains(got, "收尾") {
		t.Errorf("工具后正文缺失: %q", got)
	}
}

func TestRunTurnHotkeyAfterTool(t *testing.T) {
	dev := newFakeTerm()
	hit := atomic.Int32{}
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if n.Add(1) == 1 {
			time.Sleep(120 * time.Millisecond)
			sseChunk(w, map[string]any{"reasoning_content": "工具前思考"})
			sseToolCall(w, "call-1", "probe")
			dev.pushCtrlO()
		} else {
			sseChunk(w, map[string]any{"reasoning_content": "工具后思考\n"})
			sseChunk(w, map[string]any{"content": "收尾"})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	probe := agent.NewTool("probe", "占位", `{"type":"object"}`,
		func(ctx context.Context, args string) agent.ToolResult {
			hit.Add(1)
			return agent.ToolResult{Text: "ok"}
		})
	a := newAskAgent(t, srv.URL, probe)
	r, out, _ := newTestREPLAgent(t, a, dev)
	r.showReasoning = false
	r.ask("跑工具")
	if hit.Load() != 1 {
		t.Fatalf("工具应执行一次: %d", hit.Load())
	}
	if got := term.Strip(out.String()); !strings.Contains(got, "工具后思考") {
		t.Errorf("工具后的热键仍应生效（回合内不中途退订）: %q", got)
	}
}

func TestRunTurnTailResponseRendered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sseChunk(w, map[string]any{"content": "正文内容"})
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	a := newAskAgent(t, srv.URL)
	dev := newFakeTerm()
	r, out, _ := newTestREPLAgent(t, a, dev)
	r.showReasoning = false
	r.ask("你好")
	got := term.Strip(out.String())
	if !strings.Contains(got, "正文内容") {
		t.Errorf("正文缺失: %q", got)
	}
	if !strings.Contains(got, "↳") {
		t.Errorf("回合末 EventResponse 应渲染状态行（尾事件不丢）: %q", got)
	}
}

func TestTurnReasoningEndClosesSeg(t *testing.T) {
	r, out, _ := newTestREPL(t, newFakeTerm())
	r.showReasoning = true
	tn := r.beginTurn(func() {})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "第一段\n"})
	if got := term.Strip(out.String()); !strings.Contains(got, "第一段") {
		t.Fatalf("段内 delta 应上屏: %q", got)
	}
	tn.Handle(agent.Event{Kind: agent.EventReasoningEnd})
	if got := term.Strip(out.String()); !strings.Contains(got, "思考结束") {
		t.Fatalf("段结束事件应收尾当前块: %q", got)
	}
	out.Reset()
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "第二段\n"})
	got := term.Strip(out.String())
	if !strings.Contains(got, "思考") || !strings.Contains(got, "第二段") {
		t.Errorf("段结束后新 delta 应另起一段: %q", got)
	}
	if strings.Contains(got, "第一段") {
		t.Errorf("新段不应重放旧段内容: %q", got)
	}
	tn.End(nil)
}

func TestTurnReasoningEndResetsSegBuf(t *testing.T) {
	r, out, _ := newTestREPL(t, newFakeTerm())
	r.showReasoning = false
	tn := r.beginTurn(func() {})
	tn.Handle(agent.Event{Kind: agent.EventReasoning, Text: "旧段思考"})
	tn.Handle(agent.Event{Kind: agent.EventReasoningEnd})
	out.Reset()
	tn.Hotkey(readline.KeyEvent{Code: readline.KeyCtrlO})
	got := term.Strip(out.String())
	if strings.Contains(got, "旧段思考") {
		t.Errorf("段结束事件应清本段留存的原文、开档不再重放: %q", got)
	}
	if !strings.Contains(got, "下一段生效") {
		t.Errorf("段已结束应按空段提示: %q", got)
	}
	tn.End(nil)
}
