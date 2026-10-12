package repl

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/attach"
	"github.com/LaoQi/tanya/ctty"
	"github.com/LaoQi/tanya/readline"
)

func entryOf(t *testing.T, a *agent.Agent) attach.Entry {
	t.Helper()
	dir, ok := a.RunDir()
	if !ok {
		t.Fatal("应有 run 目录")
	}
	list, err := attach.List(dir)
	if err != nil || len(list) != 1 {
		t.Fatalf("注册项异常: %v %d", err, len(list))
	}
	return list[0]
}

func waitFor(t *testing.T, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}

func TestAttachRegistersEntry(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	dir, ok := a.RunDir()
	if !ok {
		t.Fatal("应有 run 目录")
	}
	term := newFakeTerm()
	r, _, _ := newTestREPLAgent(t, a, term)
	r.startAttach()
	t.Cleanup(r.stopAttach)
	e := entryOf(t, a)
	if e.PID != os.Getpid() || e.State != attach.StateIdle {
		t.Fatalf("注册项字段异常: %+v", e)
	}
	if e.Session == "" || e.Sock == "" {
		t.Fatalf("注册项缺字段: %+v", e)
	}
	if e.Path != attach.EntryPath(dir, os.Getpid()) {
		t.Fatalf("注册项路径应指向 run 目录: %q", e.Path)
	}
	if e.Title == "" {
		t.Error("标题不应为空")
	}
	r.setBusy(true)
	if got := entryOf(t, a).State; got != attach.StateBusy {
		t.Fatalf("忙碌态未写入: %q", got)
	}
	if entryOf(t, a).TurnStarted.IsZero() {
		t.Error("忙碌态应带回合起点")
	}
	r.setBusy(false)
	if got := entryOf(t, a).State; got != attach.StateIdle {
		t.Fatalf("空闲态未写入: %q", got)
	}
	r.stopAttach()
	if list, _ := attach.List(dir); len(list) != 0 {
		t.Fatal("停止后注册项应删除")
	}
}

func TestTakeoverIdleHandsOverAndExits(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	term.wait = make(chan struct{})
	defer close(term.wait)
	r, out, _ := newTestREPLAgent(t, a, term)

	runDone := make(chan error, 1)
	go func() { runDone <- r.Run() }()
	var e attach.Entry
	waitFor(t, 2*time.Second, func() bool {
		dir, ok := a.RunDir()
		if !ok {
			return false
		}
		list, err := attach.List(dir)
		if err != nil || len(list) != 1 {
			return false
		}
		e = list[0]
		return true
	})

	c, err := attach.Open(e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()
	res, err := c.Takeover(false)
	if err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	if !res.OK || res.Session == "" || res.File == "" || res.Workspace == "" {
		t.Fatalf("接管结果异常: %+v", res)
	}
	if res.Protocol == "" || res.Messages != 1 {
		t.Fatalf("接管结果字段异常（应已含接管事实行）: %+v", res)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run 应正常退出: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("接管后 Run 未退出")
	}
	if !strings.Contains(out.String(), "已交由另一进程接管") {
		t.Errorf("缺少交接行: %q", out.String())
	}
	if got := a.History(); len(got) != 1 || got[0].Content != agent.MsgTakenOverNotice {
		t.Fatalf("应留下接管事实行: %+v", got)
	}
	data, err := os.ReadFile(res.File)
	if err != nil {
		t.Fatalf("会话文件应已落盘: %v", err)
	}
	if !strings.Contains(string(data), agent.MsgTakenOverNotice) {
		t.Error("事实行应落盘")
	}
	dir, _ := a.RunDir()
	if list, _ := attach.List(dir); len(list) != 0 {
		t.Fatal("移交后注册项应删除")
	}
}

func pendingTakeover(t *testing.T, r *REPL, e attach.Entry, force bool) (*attach.Client, chan attach.Result) {
	t.Helper()
	c, err := attach.Open(e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	res := make(chan attach.Result, 1)
	go func() {
		got, err := c.Takeover(force)
		if err != nil {
			got = attach.Result{Code: err.Error()}
		}
		res <- got
	}()
	waitFor(t, 2*time.Second, r.takeoverPending)
	return c, res
}

func TestTakeoverBusyWaitsWithoutInterrupt(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, _, _ := newTestREPLAgent(t, a, term)
	r.startAttach()
	t.Cleanup(r.stopAttach)
	r.setBusy(true)
	e := entryOf(t, a)
	c, res := pendingTakeover(t, r, e, false)
	defer c.Close()
	if kinds := term.injectedKinds(); len(kinds) != 0 {
		t.Fatalf("忙碌且非 force 不应注入事件: %v", kinds)
	}
	if !r.settleTakeover() {
		t.Fatal("回合结束后应完成移交")
	}
	if got := <-res; !got.OK {
		t.Fatalf("接管应成功: %+v", got)
	}
}

func TestTakeoverBusyForceInterrupts(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, _, _ := newTestREPLAgent(t, a, term)
	r.startAttach()
	t.Cleanup(r.stopAttach)
	r.setBusy(true)
	e := entryOf(t, a)
	c, res := pendingTakeover(t, r, e, true)
	defer c.Close()
	kinds := term.injectedKinds()
	if len(kinds) != 1 || kinds[0] != readline.EventInterrupt {
		t.Fatalf("force 应注入中断事件: %v", kinds)
	}
	if !r.settleTakeover() {
		t.Fatal("强制接管应完成移交")
	}
	if got := <-res; !got.OK {
		t.Fatalf("接管应成功: %+v", got)
	}
}

func TestTakeoverPeerGoneKeepsSession(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, out, _ := newTestREPLAgent(t, a, term)
	r.startAttach()
	t.Cleanup(r.stopAttach)
	r.setBusy(true)
	e := entryOf(t, a)
	c, _ := pendingTakeover(t, r, e, false)
	before := len(a.History())
	c.Close()
	waitFor(t, 2*time.Second, func() bool { return !r.takeoverPending() })
	if r.settleTakeover() {
		t.Fatal("对端已断开，不应完成移交")
	}
	if got := len(a.History()); got != before {
		t.Fatalf("取消不应改写会话: %d → %d", before, got)
	}
	if !strings.Contains(out.String(), "接管请求已取消") {
		t.Errorf("应提示已取消: %q", out.String())
	}
	if r.takeoverPending() {
		t.Error("取消后应清掉挂起请求")
	}
}

// 对端断开即释放挂起：后续请求（含 force）不再被 in-progress 挡住。
func TestTakeoverCanceledRequestFreesPending(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, out, _ := newTestREPLAgent(t, a, term)
	r.startAttach()
	t.Cleanup(r.stopAttach)
	r.setBusy(true)
	e := entryOf(t, a)

	first, _ := pendingTakeover(t, r, e, false)
	first.Close()
	waitFor(t, 2*time.Second, func() bool { return !r.takeoverPending() })

	second, res := pendingTakeover(t, r, e, true)
	defer second.Close()
	if !r.takeoverPending() {
		t.Fatal("断开对端的挂起不应继续占位")
	}
	if len(term.injectedKinds()) != 1 {
		t.Fatalf("force 请求应被受理并注入中断: %v", term.injectedKinds())
	}
	if !r.settleTakeover() {
		t.Fatal("新请求应能完成移交")
	}
	if got := <-res; !got.OK {
		t.Fatalf("接管应成功: %+v", got)
	}
	if n := strings.Count(out.String(), MsgAttachCanceled); n != 1 {
		t.Fatalf("取消提示应恰好一次，实际 %d: %q", n, out.String())
	}
}

// 陈旧挂起（对端已断、serve goroutine 尚未摘除）不得挡住新请求。
func TestTakeoverStalePendingDoesNotBlock(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, _, _ := newTestREPLAgent(t, a, term)
	r.startAttach()
	t.Cleanup(r.stopAttach)
	r.setBusy(true)
	e := entryOf(t, a)

	gone := make(chan struct{})
	stale := &handover{gone: gone, deliver: make(chan attach.Result), outcome: make(chan error, 1), abort: make(chan struct{})}
	r.takeMu.Lock()
	r.takeH = stale
	r.takeMu.Unlock()
	close(gone)

	cl, err := attach.Open(e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer cl.Close()
	res := make(chan attach.Result, 1)
	go func() {
		got, err := cl.Takeover(false)
		if err != nil {
			got = attach.Result{Code: err.Error()}
		}
		res <- got
	}()
	waitFor(t, 2*time.Second, func() bool {
		r.takeMu.Lock()
		defer r.takeMu.Unlock()
		return r.takeH != nil && r.takeH != stale
	})
	if !r.settleTakeover() {
		t.Fatal("应完成移交")
	}
	if got := <-res; !got.OK {
		t.Fatalf("接管应成功: %+v", got)
	}
}

func TestTakeoverRejectsNoSaveAndInProgress(t *testing.T) {
	saveAgent := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, _, _ := newTestREPLAgent(t, saveAgent, term)
	r.startAttach()
	r.setBusy(true)
	e := entryOf(t, saveAgent)
	c, _ := pendingTakeover(t, r, e, false)
	second, err := attach.Open(e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	res, err := second.Takeover(false)
	second.Close()
	if err != nil {
		t.Fatalf("第二个请求应收到应答: %v", err)
	}
	if res.OK || res.Code != attach.CodeInProgress {
		t.Fatalf("并发接管应被拒: %+v", res)
	}
	c.Close()
	r.settleTakeover()
	r.stopAttach()

	noSave := newSessTestAgent(t, t.TempDir(), agent.NoSave(true))
	r2, _, _ := newTestREPLAgent(t, noSave, newFakeTerm())
	r2.startAttach()
	if _, ok := noSave.RunDir(); ok {
		t.Fatal("不落盘实例不应有 run 目录")
	}
	if r2.attachSrv != nil {
		t.Fatal("不落盘实例不应注册")
	}
}

func TestSessionTitleSkipsNoticeLines(t *testing.T) {
	msgs := []agent.Message{
		{Role: "user", Content: agent.MsgTakenOverNotice},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "重构 attach 设计"},
	}
	if got := sessionTitle(msgs, "/tmp/ws"); got != "重构 attach 设计" {
		t.Fatalf("应跳过事实行取真实首条: %q", got)
	}
	if got := sessionTitle([]agent.Message{{Role: "user", Content: agent.MsgInterruptNotice}}, "/tmp/ws"); got != "tanya · ws" {
		t.Fatalf("全为事实行时应回落工作区名: %q", got)
	}
}

// force 接管被取消后，注入的中断事件应被清掉（不空中断下一行输入）；非 force 取消不涉及中断。
func TestTakeoverForceCancelDrainsStaleInterrupt(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, _, _ := newTestREPLAgent(t, a, term)
	r.startAttach()
	t.Cleanup(r.stopAttach)
	r.setBusy(true)
	e := entryOf(t, a)

	c, _ := pendingTakeover(t, r, e, true)
	c.Close()
	waitFor(t, 2*time.Second, func() bool {
		if r.takeoverPending() {
			return false
		}
		kinds := term.drainedKinds()
		return len(kinds) == 1 && kinds[0] == readline.EventInterrupt
	})
	if r.settleTakeover() {
		t.Fatal("对端已断开，不应完成移交")
	}

	b := newSessTestAgent(t, t.TempDir())
	term2 := newFakeTerm()
	r2, _, _ := newTestREPLAgent(t, b, term2)
	r2.startAttach()
	t.Cleanup(r2.stopAttach)
	r2.setBusy(true)
	e2 := entryOf(t, b)
	c2, _ := pendingTakeover(t, r2, e2, false)
	c2.Close()
	waitFor(t, 2*time.Second, func() bool { return !r2.takeoverPending() })
	if kinds := term2.drainedKinds(); len(kinds) != 0 {
		t.Fatalf("非 force 取消不应涉及中断事件: %v", kinds)
	}
}

// 事实行写不进（会话目录不可写）→ 应答 io-error、解冻回滚、继续运行。
func TestTakeoverAppendNoticeFailureFailsHandover(t *testing.T) {
	if ctty.IsRoot() {
		t.Skip("root 下 chmod 不拦截写入")
	}
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, out, errb := newTestREPLAgent(t, a, term)
	r.startAttach()
	t.Cleanup(r.stopAttach)
	e := entryOf(t, a)

	sessDir, ok := a.SessionDir()
	if !ok {
		t.Fatal("应有会话目录")
	}
	if err := os.Chmod(sessDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessDir, 0o700) })

	c, res := pendingTakeover(t, r, e, false)
	defer c.Close()
	if r.settleTakeover() {
		t.Fatal("事实行写不进不应完成移交")
	}
	got := <-res
	if got.OK || got.Code != attach.CodeIOError {
		t.Fatalf("应答应为 io-error: %+v", got)
	}
	if errb.String() == "" {
		t.Error("应报告写入错误")
	}
	if !strings.Contains(out.String(), MsgAttachTakeoverFailed) {
		t.Errorf("应提示接管未成立: %q", out.String())
	}
	if err := os.Chmod(sessDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.AppendNotice("after"); err != nil {
		t.Fatalf("回滚后应解冻可写: %v", err)
	}
	found := false
	for _, m := range a.History() {
		if m.Content == agent.MsgTakenOverFailed {
			found = true
		}
	}
	if !found {
		t.Error("回滚应补接管未成立事实行")
	}
}

func TestRebindAttachKeepsServiceUsable(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, _, _ := newTestREPLAgent(t, a, term)
	if r.attachSrv != nil {
		t.Fatal("未启动不应注册")
	}
	r.rebindAttach()
	if r.attachSrv != nil {
		t.Fatal("未注册时 rebind 应无操作")
	}
	r.startAttach()
	t.Cleanup(r.stopAttach)
	r.rebindAttach()
	if r.attachSrv == nil {
		t.Fatal("rebind 后应有新服务")
	}
	r.setBusy(true)
	e := entryOf(t, a)
	c, res := pendingTakeover(t, r, e, false)
	defer c.Close()
	if !r.settleTakeover() {
		t.Fatal("重绑后应能完成移交")
	}
	if got := <-res; !got.OK {
		t.Fatalf("接管应成功: %+v", got)
	}
}
