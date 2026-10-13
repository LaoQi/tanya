package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/attach"
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

func stepServer(t *testing.T, steps ...func(w http.ResponseWriter)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		i := int(n.Add(1)) - 1
		switch {
		case i < len(steps):
			steps[i](w)
		default:
			sseChunk(w, map[string]any{"content": "多余请求"})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func probeTool(fn func() string) agent.Tool {
	return agent.NewTool("ws_probe", "测试桩：回报当前工作区（同 run_shell 的活闭包口径）",
		`{"type":"object","properties":{}}`,
		func(context.Context, string) agent.ToolResult { return agent.ToolResult{Text: fn()} })
}

func rmDirTool() agent.Tool {
	return agent.NewTool("rm_dir", "测试桩：删除目录（制造交接目标失效）",
		`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
		func(_ context.Context, argsJSON string) agent.ToolResult {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
				return agent.ToolResult{Text: err.Error()}
			}
			if err := os.RemoveAll(args.Path); err != nil {
				return agent.ToolResult{Text: err.Error()}
			}
			return agent.ToolResult{Text: "removed"}
		})
}

func TestNextSessionSwitchesWorkspace(t *testing.T) {
	dirB := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirB, ".tanya"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv, n := stepServer(t,
		func(w http.ResponseWriter) {
			sseToolArgs(w, "call-1", "next_session", fmt.Sprintf(`{"summary":"去 B","workspace":%q}`, dirB))
		},
		func(w http.ResponseWriter) { sseChunk(w, map[string]any{"content": "已交接"}) },
		func(w http.ResponseWriter) { sseToolArgs(w, "call-2", "ws_probe", `{}`) },
		func(w http.ResponseWriter) { sseChunk(w, map[string]any{"content": "接着干"}) },
	)
	var probe string
	var holder *agent.Agent
	a := newNextAgent(t, srv.URL, agent.WithTools(probeTool(func() string {
		if holder == nil {
			return ""
		}
		return holder.Workspace()
	})))
	holder = a
	from := a.Workspace()
	before, _ := a.SessionDir()
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	r.ask("干活")

	if got := term.Strip(errb.String()); got != "" {
		t.Fatalf("交接不应报错: %q", got)
	}
	text := term.Strip(out.String())
	if !strings.Contains(text, "工作区已切换: "+initPath(from)+" → "+initPath(dirB)) {
		t.Errorf("收尾块应打印工作区切换: %q", text)
	}
	if a.Workspace() != dirB {
		t.Fatalf("工作区应为目标: %q", a.Workspace())
	}
	dir, ok := a.SessionDir()
	esc := strings.ReplaceAll(strings.TrimPrefix(dirB, string(filepath.Separator)), string(filepath.Separator), "-")
	if !ok || dir == before || !strings.Contains(dir, esc) {
		t.Errorf("新会话应落目标工作区: got %q ok=%v（切换前 %q，目标 %q）", dir, ok, before, dirB)
	}
	if n.Load() != 4 {
		t.Errorf("续跑轮应在切换后的工作区执行：请求数应为 4，实为 %d", n.Load())
	}
	h := a.History()
	for _, msg := range h {
		if msg.Role == "tool" && msg.Name == "ws_probe" {
			probe = msg.Content
		}
	}
	if probe != dirB {
		t.Errorf("续跑轮的工具应看到新工作区: got %q want %q", probe, dirB)
	}
}

func TestNextSessionWorkspaceFailedAborts(t *testing.T) {
	dirB := t.TempDir()
	srv, n := stepServer(t,
		func(w http.ResponseWriter) {
			sseToolArgs(w, "call-1", "next_session", fmt.Sprintf(`{"summary":"去 B","workspace":%q}`, dirB))
		},
		func(w http.ResponseWriter) {
			sseToolArgs(w, "call-2", "rm_dir", fmt.Sprintf(`{"path":%q}`, dirB))
		},
		func(w http.ResponseWriter) { sseChunk(w, map[string]any{"content": "收尾"}) },
	)
	a := newNextAgent(t, srv.URL, agent.WithTools(rmDirTool()))
	from := a.Workspace()
	oldID := a.SessionID()
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	past := time.Now().Add(-time.Hour)
	r.started = past
	r.ask("干活")

	text := term.Strip(errb.String())
	if !strings.Contains(text, "本次交接未成立") || !strings.Contains(text, initPath(from)) {
		t.Fatalf("应报告交接未成立并标出当前工作区: %q", text)
	}
	if strings.Contains(term.Strip(out.String()), "已移交新会话") {
		t.Errorf("作废的交接不应打印收尾块: %q", out.String())
	}
	if a.Workspace() != from {
		t.Errorf("工作区不应变: %q", a.Workspace())
	}
	if a.SessionID() != oldID {
		t.Errorf("会话不应轮转: %q vs %q", a.SessionID(), oldID)
	}
	if n.Load() != 3 {
		t.Errorf("交接作废应停下等输入（不续跑）：请求数应为 3，实为 %d", n.Load())
	}
	if !r.started.Equal(past) {
		t.Errorf("会话未变不应重置计时起点: %v", r.started)
	}
	h := a.History()
	last := h[len(h)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "会话交接未成立") {
		t.Errorf("应把未成立事实行留给后续回合: %+v", last)
	}
	old := readSessionLines(t, a.SessionFile())
	for _, msg := range old {
		if strings.HasPrefix(msg.Content, "[会话已移交至 ") {
			t.Errorf("落盘不应有移交事实行: %+v", msg)
		}
	}
}

func TestHandleNextWorkspaceFailedKeepsPrompt(t *testing.T) {
	dirB := t.TempDir()
	dirC := t.TempDir()
	srv, n := stepServer(t,
		func(w http.ResponseWriter) {
			sseToolArgs(w, "call-1", "next_session", fmt.Sprintf(`{"summary":"去 C","workspace":%q}`, dirC))
		},
		func(w http.ResponseWriter) {
			sseToolArgs(w, "call-2", "rm_dir", fmt.Sprintf(`{"path":%q}`, dirC))
		},
		func(w http.ResponseWriter) { sseChunk(w, map[string]any{"content": "收尾"}) },
	)
	a := newNextAgent(t, srv.URL, agent.WithTools(rmDirTool()))
	if err := a.SwitchWorkspace(dirB); err != nil {
		t.Fatal(err)
	}
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/next 继续做 C")
	if !strings.Contains(term.Strip(errb.String()), "本次交接未成立") {
		t.Errorf("应报告交接未成立: %q", errb.String())
	}
	if got := term.Strip(out.String()); strings.Contains(got, MsgHandoffNone) {
		t.Errorf("失败路径不应再提示未生成摘要: %q", got)
	}
	if n.Load() != 3 {
		t.Errorf("带参命令在交接作废后不应执行参数: %d", n.Load())
	}
	if r.nextPending || r.nextInput != "" {
		t.Error("命令状态应已清理")
	}
	if a.Workspace() != dirB {
		t.Errorf("工作区不应变: %q", a.Workspace())
	}
}

func TestNextSessionSwitchesWorkspaceRebindsAttach(t *testing.T) {
	dirB := t.TempDir()
	srv, n := stepServer(t,
		func(w http.ResponseWriter) {
			sseToolArgs(w, "call-1", "next_session", fmt.Sprintf(`{"summary":"去 B","workspace":%q}`, dirB))
		},
		func(w http.ResponseWriter) { sseChunk(w, map[string]any{"content": "已交接"}) },
		func(w http.ResponseWriter) { sseChunk(w, map[string]any{"content": "接着干"}) },
	)
	a := newNextAgent(t, srv.URL)
	r, _, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.startAttach()
	t.Cleanup(r.stopAttach)
	if r.attachSrv == nil {
		t.Skip("本平台不支持本地控制服务")
	}
	oldDir := r.attachDir
	r.ask("干活")

	if a.Workspace() != dirB {
		t.Fatalf("工作区应为目标: %q", a.Workspace())
	}
	if n.Load() != 3 {
		t.Errorf("续跑轮应在切换后执行：请求数应为 3，实为 %d", n.Load())
	}
	if r.attachDir == oldDir {
		t.Errorf("注册目录应随工作区重绑: %q", r.attachDir)
	}
	if e := entryOf(t, a); e.Session != a.SessionID() {
		t.Errorf("新工作区注册项应指向新会话: %+v（当前 %q）", e, a.SessionID())
	}
	if list, err := attach.List(oldDir); err != nil || len(list) != 0 {
		t.Errorf("旧工作区注册项应清除: %v %d", err, len(list))
	}
}

func TestHandleNextInputRunsInSwitchedWorkspace(t *testing.T) {
	dirB := t.TempDir()
	srv, n := stepServer(t,
		func(w http.ResponseWriter) {
			sseToolArgs(w, "call-1", "next_session", fmt.Sprintf(`{"summary":"去 B","workspace":%q}`, dirB))
		},
		func(w http.ResponseWriter) { sseChunk(w, map[string]any{"content": "已交接"}) },
		func(w http.ResponseWriter) { sseToolArgs(w, "call-2", "ws_probe", `{}`) },
		func(w http.ResponseWriter) { sseChunk(w, map[string]any{"content": "接着干"}) },
	)
	var holder *agent.Agent
	a := newNextAgent(t, srv.URL, agent.WithTools(probeTool(func() string {
		if holder == nil {
			return ""
		}
		return holder.Workspace()
	})))
	holder = a
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/next 在 B 里继续做")

	if n.Load() != 4 {
		t.Errorf("指令轮应在切换后的工作区执行：请求数应为 4，实为 %d", n.Load())
	}
	if a.Workspace() != dirB {
		t.Fatalf("工作区应为目标: %q", a.Workspace())
	}
	if !strings.Contains(term.Strip(out.String()), "工作区已切换") {
		t.Errorf("应打印工作区切换行: %q", out.String())
	}
	h := a.History()
	if len(h) < 3 || h[1].Role != "user" || h[1].Content != "在 B 里继续做" {
		t.Fatalf("指令应为新会话第二条 user 输入: %+v", h)
	}
	for _, msg := range h {
		if msg.Role == "tool" && msg.Name == "ws_probe" && msg.Content != dirB {
			t.Errorf("指令轮工具应看到新工作区: got %q want %q", msg.Content, dirB)
		}
	}
}
