package repl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/term"
)

func sseToolArgs(w http.ResponseWriter, id, name, args string) {
	b, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{
		"tool_calls": []map[string]any{{
			"index": 0, "id": id, "type": "function",
			"function": map[string]any{"name": name, "arguments": args},
		}},
	}}}})
	fmt.Fprintf(w, "data: %s\n\n", b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func newNextAgent(t *testing.T, baseURL string, opts ...agent.Option) *agent.Agent {
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
	a, err := agent.New(cfg, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func newNextServer(t *testing.T, n *atomic.Int32, first func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		switch n.Add(1) {
		case 1:
			first(w)
		case 2:
			sseChunk(w, map[string]any{"content": "已交接"})
		default:
			sseChunk(w, map[string]any{"content": "接着干"})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHandleNextWithInputContinues(t *testing.T) {
	var n atomic.Int32
	srv := newNextServer(t, &n, func(w http.ResponseWriter) {
		sseToolArgs(w, "call-1", "next_session", `{"summary":"做了 A","continue":true}`)
	})
	a := newNextAgent(t, srv.URL)
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	if exit := r.handleCommand("/next 先跑测试"); exit {
		t.Fatal("不应退出")
	}
	got := term.Strip(out.String())
	if !strings.Contains(got, "已移交新会话") {
		t.Errorf("应打印移交块: %q", got)
	}
	if errb.String() != "" {
		t.Errorf("不应有错误输出: %q", errb.String())
	}
	if n.Load() != 3 {
		t.Errorf("应为 摘要轮 + 收尾轮 + 参数轮 共 3 次请求: %d", n.Load())
	}
	h := a.History()
	if len(h) != 3 {
		t.Fatalf("新会话应为 交接+输入+回答 3 条: %+v", h)
	}
	if h[0].Role != "user" || !strings.Contains(h[0].Content, "做了 A") {
		t.Errorf("首条应为交接摘要: %+v", h[0])
	}
	if h[1].Role != "user" || h[1].Content != "先跑测试" {
		t.Errorf("次条应为用户指令: %+v", h[1])
	}
	if h[2].Role != "assistant" || h[2].Content != "接着干" {
		t.Errorf("末条应为回答: %+v", h[2])
	}
}

func TestHandleNextNoInputWaits(t *testing.T) {
	var n atomic.Int32
	srv := newNextServer(t, &n, func(w http.ResponseWriter) {
		sseToolArgs(w, "call-1", "next_session", `{"summary":"摘要","continue":true}`)
	})
	a := newNextAgent(t, srv.URL)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/next")
	if !strings.Contains(term.Strip(out.String()), "已移交新会话") {
		t.Errorf("应打印移交块: %q", out.String())
	}
	if n.Load() != 2 {
		t.Errorf("命令无参不续跑：请求数应为 2，实为 %d", n.Load())
	}
	if h := a.History(); len(h) != 1 || !strings.Contains(h[0].Content, "摘要") {
		t.Errorf("应停在新会话等待输入: %+v", h)
	}
}

func TestNextSessionAutoContinue(t *testing.T) {
	var n atomic.Int32
	srv := newNextServer(t, &n, func(w http.ResponseWriter) {
		sseToolArgs(w, "call-1", "next_session", `{"summary":"摘要"}`)
	})
	a := newNextAgent(t, srv.URL)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.ask("干活")
	if !strings.Contains(term.Strip(out.String()), "已移交新会话") {
		t.Errorf("模型自调也应打印移交块: %q", out.String())
	}
	if n.Load() != 3 {
		t.Errorf("continue 缺省应自动续跑：请求数应为 3，实为 %d", n.Load())
	}
	h := a.History()
	if len(h) != 2 || h[1].Content != "接着干" {
		t.Errorf("续跑轮应追加在交接摘要之后: %+v", h)
	}
}

func TestNextSessionContinueFalseWaits(t *testing.T) {
	var n atomic.Int32
	srv := newNextServer(t, &n, func(w http.ResponseWriter) {
		sseToolArgs(w, "call-1", "next_session", `{"summary":"摘要","continue":false}`)
	})
	a := newNextAgent(t, srv.URL)
	r, _, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.ask("干活")
	if n.Load() != 2 {
		t.Errorf("continue:false 不应续跑：请求数应为 2，实为 %d", n.Load())
	}
	if h := a.History(); len(h) != 1 {
		t.Errorf("应停在新会话: %+v", h)
	}
}

func TestHandleNextWithoutHandoff(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n.Add(1)
		sseChunk(w, map[string]any{"content": "没什么可交接"})
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	a := newNextAgent(t, srv.URL)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/next 先跑测试")
	got := term.Strip(out.String())
	if !strings.Contains(got, MsgHandoffNone) {
		t.Errorf("未交接应提示: %q", got)
	}
	if strings.Contains(got, "已移交新会话") {
		t.Errorf("未交接不应打印移交块: %q", got)
	}
	if n.Load() != 1 {
		t.Errorf("未交接不应有后续请求: %d", n.Load())
	}
	if h := a.History(); len(h) != 2 || h[0].Content != agent.MsgHandoffPrompt {
		t.Errorf("本轮应作为普通对话处理: %+v", h)
	}
	if r.nextPending || r.nextInput != "" {
		t.Error("命令状态应已清理")
	}
}

func TestHandleNextPersistsSessions(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		switch n.Add(1) {
		case 1:
			sseChunk(w, map[string]any{"content": "好"})
		case 2:
			sseToolArgs(w, "call-1", "next_session", `{"summary":"摘要"}`)
		default:
			sseChunk(w, map[string]any{"content": "已交接"})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	a := newNextAgent(t, srv.URL)
	r, _, errb := newTestREPLAgent(t, a, newFakeTerm())
	r.ask("干活")
	if errb.String() != "" {
		t.Fatalf("第一轮不应报错: %q", errb.String())
	}
	oldFile := a.SessionFile()
	if oldFile == "" {
		t.Fatal("第一轮后应有会话文件")
	}
	r.handleCommand("/next")
	if got := term.Strip(errb.String()); got != "" {
		t.Fatalf("交接不应报错: %q", got)
	}
	fresh := a.SessionFile()
	if fresh == "" || fresh == oldFile {
		t.Fatalf("应交接到新的会话文件: %q → %q", oldFile, fresh)
	}
	old := readSessionLines(t, oldFile)
	if last := old[len(old)-1]; last.Role != "user" || !strings.HasPrefix(last.Content, "[会话已移交至 ") {
		t.Errorf("旧会话末条应为移交事实行: %+v", last)
	}
}

func readSessionLines(t *testing.T, path string) []agent.Message {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var msgs []agent.Message
	for {
		var m agent.Message
		if err := dec.Decode(&m); err != nil {
			break
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func TestHandleNextInputOverridesContinueFalse(t *testing.T) {
	var n atomic.Int32
	srv := newNextServer(t, &n, func(w http.ResponseWriter) {
		sseToolArgs(w, "call-1", "next_session", `{"summary":"做了 A","continue":false}`)
	})
	a := newNextAgent(t, srv.URL)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/next 继续跑测试")
	if !strings.Contains(term.Strip(out.String()), "已移交新会话") {
		t.Fatalf("应打印移交块: %q", out.String())
	}
	if n.Load() != 3 {
		t.Errorf("命令带参应忽略 continue:false 而续跑：请求数应为 3，实为 %d", n.Load())
	}
	h := a.History()
	if len(h) != 3 {
		t.Fatalf("新会话应为 交接+指令+回答 3 条: %+v", h)
	}
	if h[1].Role != "user" || h[1].Content != "继续跑测试" {
		t.Errorf("指令应作为新会话首条输入: %+v", h[1])
	}
	if h[2].Content != "接着干" {
		t.Errorf("指令应被立即执行: %+v", h[2])
	}
}

func TestHandleNextRejectsArchiveReadOnly(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	archiveOldSession(t, a, dir, "20260101-010000")
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	out.Reset()
	r.handleCommand("/load 20260101-010000")
	out.Reset()
	errb.Reset()
	want := fmt.Sprintf(MsgHandoffReadOnlyFmt, "20260101-010000")
	if exit := r.handleCommand("/next"); exit {
		t.Fatal("不应退出")
	}
	got := term.Strip(out.String())
	if !strings.Contains(got, want) {
		t.Errorf("无参 /next 应提示归档只读: %q", got)
	}
	if strings.Contains(got, "已移交新会话") || r.nextPending || r.nextInput != "" {
		t.Errorf("被拒的 /next 不应进入交接流程: %q pending=%v input=%q", got, r.nextPending, r.nextInput)
	}
	if errb.String() != "" {
		t.Errorf("拒绝不应产生错误输出: %q", errb.String())
	}
	out.Reset()
	r.handleCommand("/next 继续干活")
	if !strings.Contains(term.Strip(out.String()), want) {
		t.Errorf("带参 /next 同样应被拒: %q", out.String())
	}
	if r.nextPending || r.nextInput != "" {
		t.Error("带参被拒也不应置状态")
	}
	if id, ok := a.ArchiveReadOnly(); !ok || id != "20260101-010000" {
		t.Errorf("会话态不应改变: %q %v", id, ok)
	}
}

func TestHandoffResetsStarted(t *testing.T) {
	var n atomic.Int32
	srv := newNextServer(t, &n, func(w http.ResponseWriter) {
		sseToolArgs(w, "call-1", "next_session", `{"summary":"摘要"}`)
	})
	a := newNextAgent(t, srv.URL)
	r, _, _ := newTestREPLAgent(t, a, newFakeTerm())
	past := time.Now().Add(-time.Hour)
	r.started = past
	r.ask("干活")
	if !r.started.After(past) {
		t.Errorf("交接后应重置计时起点（旧会话时长不延续）: %v", r.started)
	}
	if d := time.Since(r.started); d > time.Minute {
		t.Errorf("计时起点应接近交接时刻: %v", d)
	}
}

func TestHandoffBlockTextExact(t *testing.T) {
	var n atomic.Int32
	srv := newNextServer(t, &n, func(w http.ResponseWriter) {
		sseToolArgs(w, "call-1", "next_session", `{"summary":"做了 A\n下一步 B","continue":false}`)
	})
	a := newNextAgent(t, srv.URL)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.started = time.Now().Add(-90 * time.Second)
	oldID := a.SessionID()
	oldFile := a.SessionFile()
	r.handleCommand("/next")
	got := term.Strip(out.String())
	newID := a.SessionID()
	if newID == "" || newID == oldID {
		t.Fatalf("应交接到新会话: %q → %q", oldID, newID)
	}
	if want := fmt.Sprintf(MsgHandoffBlockFmt, newID, 2); !strings.Contains(got, want) {
		t.Errorf("收尾块应含 %q（摘要两行）: %q", want, got)
	}
	if want := fmt.Sprintf("会话 %s · 时长 ", oldID); !strings.Contains(got, want) {
		t.Errorf("收尾块应含旧会话 id 行 %q: %q", want, got)
	}
	if !strings.Contains(got, "用量 无（未收到 API usage）") {
		t.Errorf("收尾块应含用量行: %q", got)
	}
	if !strings.Contains(got, fmt.Sprintf(MsgFarewellFileFmt, oldFile)) {
		t.Errorf("收尾块应含旧会话文件行: %q", got)
	}
}

func TestHandoffBlockNoSave(t *testing.T) {
	var n atomic.Int32
	srv := newNextServer(t, &n, func(w http.ResponseWriter) {
		sseToolArgs(w, "call-1", "next_session", `{"summary":"摘要","continue":false}`)
	})
	a := newNextAgent(t, srv.URL, agent.NoSave(true))
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/next")
	got := term.Strip(out.String())
	if !strings.Contains(got, MsgFarewellNoFile) {
		t.Errorf("-n 收尾块应提示未写入: %q", got)
	}
	if strings.Contains(got, ".jsonl") {
		t.Errorf("-n 收尾块不应出现会话文件路径: %q", got)
	}
	if !strings.Contains(got, "（交接 1 行）") {
		t.Errorf("收尾块应含交接行数: %q", got)
	}
	if h := a.History(); len(h) != 1 {
		t.Errorf("-n 交接仍应切到新会话（内存）: %+v", h)
	}
}
