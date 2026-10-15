package repl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/attach"
	"github.com/LaoQi/tanya/readline"
)

// handover 是一次进行中的接管请求：主循环在交接点交付结果，serve goroutine 写出并回报。
type handover struct {
	force   bool
	gone    <-chan struct{}
	deliver chan attach.Result
	outcome chan error
	abort   chan struct{}
}

// attachState 是 run 目录注册项的本地快照：主循环写、serve goroutine 读，统一由 takeMu 保护。
type attachState struct {
	session   string
	title     string
	archived  string
	messages  int
	busy      bool
	turnStart time.Time
}

// startAttach 注册运行实例并起控制服务；平台或设备不支持时静默降级（不注册）。
func (r *REPL) startAttach() {
	if r.agent == nil {
		return
	}
	dir, ok := r.agent.RunDir()
	if !ok {
		return
	}
	p, ok := r.con.(readline.Poster)
	if !ok || !p.Attachable() {
		return
	}
	r.attachDir = dir
	r.attachTTY = attach.TTYName()
	r.attachStarted = time.Now()
	r.snapshot()
	srv, err := attach.Serve(dir, os.Getpid(), r)
	if err != nil {
		r.st.out.emit(KindNotice, MsgAttachDisabled)
		return
	}
	r.attachSrv = srv
	r.writeEntry()
}

// post 向输入层注入一个事件（平台/设备不支持时返回 false）。
func (r *REPL) post(kind readline.EventKind) bool {
	p, ok := r.con.(readline.Poster)
	if !ok {
		return false
	}
	return p.Post(kind)
}

func (r *REPL) stopAttach() {
	if r.attachSrv != nil {
		_ = r.attachSrv.Close()
		r.attachSrv = nil
	}
}

// rebindAttach 在工作区切换后重建注册：旧 server 关闭时一并删除旧 run 目录里的注册项。
func (r *REPL) rebindAttach() {
	if r.attachSrv == nil {
		return
	}
	r.stopAttach()
	r.startAttach()
}

// setBusy 切换回合态并刷新注册项（忙碌期间零额外写入，turn_started 由接管方本地折算）。
func (r *REPL) setBusy(busy bool) {
	if r.attachSrv == nil {
		return
	}
	r.takeMu.Lock()
	r.takeState.busy = busy
	if busy {
		r.takeState.turnStart = time.Now()
	} else {
		r.takeState.turnStart = time.Time{}
	}
	r.takeMu.Unlock()
	r.refreshState()
}

// snapshot 从 agent 现取会话信息（只允许主循环调用），保留 busy/turnStart。
func (r *REPL) snapshot() {
	r.takeMu.Lock()
	st := r.takeState
	r.takeMu.Unlock()
	st.session, st.title, st.archived, st.messages = "", "", "", 0
	if r.agent != nil {
		st.session = r.agent.SessionID()
		st.messages = len(r.agent.History())
		st.title = sessionTitle(r.agent.History(), r.agent.Workspace())
		if id, ok := r.agent.ArchiveReadOnly(); ok {
			st.archived = id
		}
	}
	r.takeMu.Lock()
	r.takeState = st
	r.takeMu.Unlock()
}

func (r *REPL) refreshState() {
	r.snapshot()
	r.writeEntry()
}

func (r *REPL) writeEntry() {
	if r.attachSrv == nil {
		return
	}
	r.takeMu.Lock()
	st := r.takeState
	r.takeMu.Unlock()
	e := attach.Entry{
		V:           attach.Version,
		PID:         os.Getpid(),
		Sock:        r.attachSrv.SockPath(),
		TTY:         r.attachTTY,
		Title:       st.title,
		State:       entryState(st),
		TurnStarted: st.turnStart,
		Session:     st.session,
		Messages:    st.messages,
		Archived:    st.archived,
		Started:     r.attachStarted,
		Updated:     time.Now(),
	}
	_ = attach.WriteEntry(r.attachDir, e)
}

func entryState(st attachState) string {
	switch {
	case st.busy:
		return attach.StateBusy
	case st.archived != "":
		return attach.StateReadOnly
	}
	return attach.StateIdle
}

// noticeLine 识别 tanya 自己写入的单行方括号事实行（接管/中断/错误），标题不取它们。
func noticeLine(s string) bool {
	return strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]")
}

// sessionTitle 生成本实例的人类可读标题：会话首条 user 消息摘要，无则回落工作区名。
func sessionTitle(msgs []agent.Message, workspace string) string {
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		if s := agent.Summarize(strings.TrimSpace(m.Content)); s != "" && !noticeLine(s) {
			return s
		}
		if len(m.Images) > 0 {
			return fmt.Sprintf(agent.MsgSessionImageSummary, len(m.Images))
		}
	}
	if workspace == "" {
		return "tanya"
	}
	return "tanya · " + filepath.Base(workspace)
}

// Takeover 实现 attach.Handler：受理一次接管请求，或立即拒绝。
func (r *REPL) Takeover(force bool, peer attach.Peer, reply func(attach.Result) error) {
	r.takeMu.Lock()
	st := r.takeState
	switch {
	case r.takeH != nil && !peerGone(r.takeH):
		r.takeMu.Unlock()
		_ = reply(rejectTakeover(attach.CodeInProgress))
		return
	case r.agent == nil || r.agent.NoSave():
		r.takeMu.Unlock()
		_ = reply(rejectTakeover(attach.CodeNoSave))
		return
	case st.archived != "":
		r.takeMu.Unlock()
		_ = reply(rejectTakeover(attach.CodeReadOnly))
		return
	}
	var stale *handover
	if r.takeH != nil {
		stale = r.takeH
		r.takeH, r.takeGone = nil, r.takeGone+1
	}
	h := &handover{
		force:   force,
		gone:    peer.Gone(),
		deliver: make(chan attach.Result),
		outcome: make(chan error, 1),
		abort:   make(chan struct{}),
	}
	r.takeH = h
	busy := st.busy
	r.takeMu.Unlock()
	if stale != nil {
		r.drainTakeover(stale)
	}

	if busy {
		if force && !r.cancelTurn() {
			r.post(readline.EventTakeover)
		}
	} else if !r.post(readline.EventTakeover) {
		r.clearHandover(h)
		_ = reply(rejectTakeover(attach.CodeBadRequest))
		return
	}
	select {
	case res := <-h.deliver:
		h.outcome <- reply(res)
	case <-h.abort:
	case <-h.gone:
		r.dropGone(h)
	}
}

func (r *REPL) setTurnCancel(fn context.CancelFunc) {
	r.turnMu.Lock()
	r.turnCancel = fn
	r.turnMu.Unlock()
}

func (r *REPL) clearTurnCancel() {
	r.turnMu.Lock()
	r.turnCancel = nil
	r.turnMu.Unlock()
}

func (r *REPL) cancelTurn() bool {
	r.turnMu.Lock()
	fn := r.turnCancel
	r.turnMu.Unlock()
	if fn == nil {
		return false
	}
	fn()
	return true
}

func rejectTakeover(code string) attach.Result { return attach.Result{Code: code} }

// peerGone 报告挂起请求的对端是否已断开（只看已关闭的 gone，不阻塞）。
func peerGone(h *handover) bool {
	select {
	case <-h.gone:
		return true
	default:
		return false
	}
}

// dropGone 摘除对端已断开的挂起请求并记一次「已取消」，由主循环报告（reportGone）。
func (r *REPL) dropGone(h *handover) bool {
	r.takeMu.Lock()
	if r.takeH != h {
		r.takeMu.Unlock()
		return false
	}
	r.takeH, r.takeGone = nil, r.takeGone+1
	r.takeMu.Unlock()
	r.drainTakeover(h)
	return true
}

// reportGone 消费已记下的「接管请求已取消」并逐次报告（只允许主循环调用，渲染保持单 goroutine）。
func (r *REPL) reportGone() {
	r.takeMu.Lock()
	n := r.takeGone
	r.takeGone -= n
	r.takeMu.Unlock()
	for i := 0; i < n; i++ {
		r.st.out.emit(KindNotice, MsgAttachCanceled)
	}
}

func (r *REPL) clearHandover(h *handover) {
	r.takeMu.Lock()
	if r.takeH == h {
		r.takeH = nil
	}
	r.takeMu.Unlock()
}

func (r *REPL) takeoverPending() bool {
	r.takeMu.Lock()
	defer r.takeMu.Unlock()
	return r.takeH != nil
}

// settleTakeover 在交接点完成移交：事实行 → 冻结 → 应答 → 退出；事实行写不进、未送达或对端取消则回滚继续运行。
func (r *REPL) settleTakeover() bool {
	r.reportGone()
	r.takeMu.Lock()
	h := r.takeH
	r.takeMu.Unlock()
	if h == nil {
		return false
	}
	select {
	case <-h.gone:
		close(h.abort)
		r.dropGone(h)
		r.reportGone()
		return false
	default:
	}
	ioFail := false
	if err := r.agent.AppendNotice(agent.MsgTakenOverNotice); err != nil {
		r.failErr(err)
		ioFail = true
	}
	r.agent.FreezeSession()
	var res attach.Result
	if ioFail {
		res = rejectTakeover(attach.CodeIOError)
	} else {
		res = r.takeoverResult()
	}
	select {
	case h.deliver <- res:
	case <-h.gone:
		r.clearHandover(h)
		close(h.abort)
		r.rollbackTakeover(h, MsgAttachCanceled)
		return false
	}
	if err := <-h.outcome; err != nil || !res.OK {
		r.clearHandover(h)
		r.rollbackTakeover(h, MsgAttachTakeoverFailed)
		return false
	}
	r.clearHandover(h)
	r.stopAttach()
	r.farewell()
	r.st.out.emit(KindDecor, MsgAttachHandedOver)
	return true
}

// rollbackTakeover 撤销未成立的移交：补事实行、解冻、刷新注册项并清残留注入事件。
func (r *REPL) rollbackTakeover(h *handover, notice string) {
	_ = r.agent.AppendNotice(agent.MsgTakenOverFailed)
	r.agent.UnfreezeSession()
	r.st.out.emit(KindNotice, notice)
	r.refreshState()
	r.drainTakeover(h)
}

// drainTakeover 清掉接管注入但未被编辑层消费的接管事件，避免回滚后空退出一行输入。
func (r *REPL) drainTakeover(*handover) {
	d, ok := r.con.(readline.Drainer)
	if !ok {
		return
	}
	d.DrainInjected(readline.EventTakeover)
}

func (r *REPL) takeoverResult() attach.Result {
	res := attach.Result{OK: true}
	res.Session = r.agent.SessionID()
	res.File = r.agent.SessionFile()
	if dir, ok := r.agent.SessionDir(); ok {
		res.SessionDir = dir
	}
	if dir, ok := r.agent.ArchiveDir(); ok {
		res.ArchiveDir = dir
	}
	res.Workspace = r.agent.Workspace()
	res.Model = r.agent.Model()
	res.Effort = r.agent.ReasoningEffort()
	res.Protocol = r.agent.ApiProtocol()
	res.Messages = len(r.agent.History())
	return res
}
