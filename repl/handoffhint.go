package repl

import (
	"context"
	"fmt"
	"sync"

	"github.com/LaoQi/battlelab/agent"
)

func contextUsed(st agent.Stats) int {
	if st.ContextTokens > st.Est {
		return st.ContextTokens
	}
	return st.Est
}

// HandoffHint 是上下文交接提醒钩子（BeforeRequestHook + SessionHook）：
// 在每次请求发出前判阈值、达阈则注入一条事实行；屏幕提示行经 REPL 的回合通道渲染。
//
// 两个切点的分工：
//   - BeforeRequest：判阈值 → AppendNotice（本轮请求就带提示）→ 投一行屏幕提示
//   - SessionEvent：任一与会话边界（新建/载入/接管/分支/换工作区/交接）重新武装
//
// 注入的两个前提（缺一不做）：拿到 ctx 里的 Agent（agent.FromContext）、有 REPL 回合在驱动
// （render 绑定了回合通道）。后者使单发 `tanya ask` 与库外直接调 Ask 的路径不参与提醒。
type HandoffHint struct {
	mu     sync.Mutex
	tokens int
	done   bool
	muted  bool
	post   func(text string, isErr bool) bool
}

// NewHandoffHint 造钩子；tokens <= 0 即关闭（钩子仍可注册，只是不做事）。
func NewHandoffHint(tokens int) *HandoffHint { return &HandoffHint{tokens: tokens} }

// render 绑定/解绑本回合的渲染通道（提交后阻塞到 REPL 渲染完成，保证与事件同序）。
func (h *HandoffHint) render(post func(string, bool) bool) {
	h.mu.Lock()
	h.post = post
	h.mu.Unlock()
}

// mute 抑制本回合的提醒：/next 命令轮与交接后的续跑轮保持既有「不提醒」语义。
func (h *HandoffHint) mute(on bool) {
	h.mu.Lock()
	h.muted = on
	h.mu.Unlock()
}

// fired 报告本会话是否已提醒过（测试与诊断用）。
func (h *HandoffHint) fired() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.done
}

func (h *HandoffHint) BeforeRequest(ctx context.Context, in agent.RequestInfo) {
	h.mu.Lock()
	skip := h.tokens <= 0 || h.done || h.muted || h.post == nil
	h.mu.Unlock()
	if skip || ctx.Err() != nil {
		return
	}
	a, ok := agent.FromContext(ctx)
	if !ok {
		return
	}
	used := contextUsed(in.Stats())
	if used < h.tokens {
		return
	}
	notice := fmt.Sprintf(MsgHandoffHintNoticeFmt, formatTokens(used), formatTokens(h.tokens))
	h.mu.Lock()
	h.done = true
	h.mu.Unlock()
	if err := a.AppendNotice(notice); err != nil {
		h.postLine(fmt.Sprintf(MsgHandoffHintFailedFmt, err), true)
		return
	}
	h.postLine(fmt.Sprintf(MsgHandoffHintFmt, formatTokens(used), formatTokens(h.tokens)), false)
}

func (h *HandoffHint) postLine(text string, isErr bool) {
	h.mu.Lock()
	post := h.post
	h.mu.Unlock()
	if post != nil {
		post(text, isErr)
	}
}

// SessionEvent 在任一会话边界重新武装（注入按会话去重，与会话生命周期同构）。
func (h *HandoffHint) SessionEvent(ctx context.Context, in agent.SessionEvent) {
	h.mu.Lock()
	h.done = false
	h.mu.Unlock()
}
