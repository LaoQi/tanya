package repl

import (
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
		UserAgent:        agent.DefaultUserAgent,
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		time.Sleep(250 * time.Millisecond)
		sseChunk(w, map[string]any{"reasoning_content": "落定的思考"})
		sseChunk(w, map[string]any{"content": "答案"})
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	a := newAskAgent(t, srv.URL)
	dev := newFakeTerm(readline.KeyEvent{Code: readline.KeyCtrlO})
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

func TestRunTurnToolAckStopsReader(t *testing.T) {
	dev := newFakeTerm(readline.KeyEvent{Code: readline.KeyCtrlO})
	dev.park = true
	var readsAtInvoke atomic.Int32
	probe := agent.NewTool("probe", "记录执行时刻", `{"type":"object"}`,
		func(ctx context.Context, args string) agent.ToolResult {
			readsAtInvoke.Store(dev.endReads.Load())
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
	r, out, _ := newTestREPLAgent(t, a, dev)
	endReadBase := dev.endReads.Load()
	r.showReasoning = true
	r.ask("跑工具")
	if readsAtInvoke.Load() <= endReadBase {
		t.Fatalf("工具执行前读循环应已 EndRead（ack 顺序）: base=%d at=%d", endReadBase, readsAtInvoke.Load())
	}
	if got := term.Strip(out.String()); !strings.Contains(got, "收尾") {
		t.Errorf("工具后正文缺失: %q", got)
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
