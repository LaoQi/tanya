package repl

import (
	"errors"
	"github.com/LaoQi/tanya/render"
	"github.com/LaoQi/tanya/render/ir"
	"github.com/LaoQi/tanya/render/markdown"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"
	"time"

	"github.com/LaoQi/tanya/agent"
)

// Kind 标记每次输出的类别，是噪音门禁与测试断言的把手（不导出包外、不进 agent.Event）。
type Kind uint8

const (
	KindContent    Kind = iota + 1 // assistant 正文（流式 + 回放）
	KindReasoning                  // 思维链（仅 rich 档且开关打开时上屏）
	KindToolBlock                  // 工具标题/正文块（含其结构性空行）
	KindToolStatus                 // 工具状态行与响应状态行（↳ …）
	KindNotice                     // 信息性文案：命令反馈、回放列表
	KindDecor                      // 纯装饰：欢迎屏、回合分隔线
	KindError                      // 错误与中断提示
	KindStatus                     // 过程状态行（等待/执行心跳）
)

// flow 是一次回合的渲染上下文：REPL 只在构造时快照 profile，渲染器与 markdown 缓冲按回合派生。
type flow struct {
	st   *streams
	prof term.Profile
	sem  theme.Semantics
	md   *markdown.MarkdownBuf
	rend render.Renderer
}

func (f *flow) emit(kind Kind, s string) { f.st.out.emit(kind, s) }

func (f *flow) mdEnabled() bool { return f.prof.TTY && f.st.decor() }

// turn 承载一次对话回合：懒补首行空行、结算 markdown、收尾文案与分隔线。
type turn struct {
	r     *REPL
	f     *flow
	start time.Time
	done  func()
	gap   bool

	reasonBuf   *markdown.MarkdownBuf
	reasonOpen  bool
	reasonStart time.Time
}

func (r *REPL) beginTurn(done func()) *turn {
	width := r.view.width()
	md := markdown.NewMarkdownBuf()
	md.SetWidth(width)
	reason := markdown.NewMarkdownBuf()
	reason.SetWidth(width)
	return &turn{
		r:     r,
		start: time.Now(),
		done:  done,
		f: &flow{
			st:   r.st,
			prof: r.prof,
			sem:  r.sem,
			md:   md,
			rend: r.rend,
		},
		reasonBuf: reason,
	}
}

func (t *turn) Handle(e agent.Event) {
	if !t.gap {
		t.gap = true
		if t.f.prof.TTY {
			t.f.emit(KindDecor, "\n")
		}
	}
	switch e.Kind {
	case agent.EventReasoning:
		if t.reasonOn() {
			t.writeReasoning(e.Text)
			return
		}
		t.r.view.Handle(e)
	case agent.EventContent:
		t.flushReason()
		t.writeContent(e.Text)
	case agent.EventToolStart:
		t.flushReason()
		t.settleMd()
		t.r.view.Handle(e)
		t.notifyNeedInput(e)
	case agent.EventResponse:
		t.flushReason()
		t.settleMd()
		t.r.view.Handle(e)
	default:
		t.r.view.Handle(e)
	}
}

// reasonOn 报告思维链是否上屏：开关打开且当前输出档可显示（见 reasonVisible）。
func (t *turn) reasonOn() bool {
	return t.r.showReasoning && t.r.reasonVisible()
}

// writeReasoning 渲染思维链 delta：首个 delta 停掉等待心跳并打开分隔块，
// 其余 delta 走与正文同一 markdown 管线（逐块上屏，畸形影响由缓冲看门狗限制在局部）。
func (t *turn) writeReasoning(s string) {
	if s == "" {
		return
	}
	t.r.view.Stop()
	if !t.reasonOpen {
		t.reasonOpen = true
		t.reasonStart = time.Now()
		t.r.print(reasonSep(t.f.sem, MsgReasonHead, 0), KindReasoning)
	}
	for _, blk := range t.reasonBuf.Write(s) {
		t.r.print(t.f.rend.Block(blk), KindReasoning)
	}
}

// flushReason 收尾思维链（幂等）：结算残留块并补带时长的下分隔符，正文随后紧接下一行。
func (t *turn) flushReason() {
	if !t.reasonOpen {
		return
	}
	for _, blk := range t.reasonBuf.Close() {
		t.r.print(t.f.rend.Block(blk), KindReasoning)
	}
	t.r.print(reasonSep(t.f.sem, MsgReasonTail, time.Since(t.reasonStart)), KindReasoning)
	t.reasonOpen = false
}

func (t *turn) writeContent(s string) {
	if !t.f.mdEnabled() {
		t.r.print(term.Sanitize(s, false), KindContent)
		return
	}
	for _, blk := range t.f.md.Write(s) {
		t.r.print(t.f.rend.Block(blk), KindContent)
	}
}

func (t *turn) settleMd() {
	for _, blk := range t.f.md.Close() {
		t.r.print(t.f.rend.Block(blk), KindContent)
	}
}

func (t *turn) End(err error) {
	dur := time.Since(t.start)
	t.r.view.Stop()
	t.flushReason()
	t.settleMd()
	if t.done != nil {
		t.done()
	}
	var ie *agent.InterruptError
	interrupted := errors.As(err, &ie)
	if err != nil {
		if interrupted {
			if ie.Kept {
				t.f.st.err.emit(KindError, MsgInterruptKept)
			} else {
				t.f.st.err.emit(KindError, MsgInterruptBare)
			}
		} else {
			t.r.failErr(err)
		}
	}
	t.f.emit(KindDecor, turnSep(t.f.prof, t.f.sem, dur))
	if !interrupted {
		t.r.notify(Notification{Reason: NotifyTurnDone, Duration: dur, Failed: err != nil})
	}
}

// notifyNeedInput 在 run_shell 主动声明 interactive（终端即将移交）时通知：
// 不探测子进程真实读取 stdin 的时刻，静默阻塞（如 cat）的虚报接受。
func (t *turn) notifyNeedInput(e agent.Event) {
	if !e.Interactive {
		return
	}
	t.r.notify(Notification{Reason: NotifyNeedInput, Tool: e.ToolName})
}

// mdBlocks 把整段文本按 markdown 管线解析为块（回放等一次性展示用，不复用回合缓冲）。
func mdBlocks(text string, cols int) []ir.Block {
	buf := markdown.NewMarkdownBuf()
	buf.SetWidth(cols)
	var blks []ir.Block
	blks = append(blks, buf.Write(text)...)
	blks = append(blks, buf.Close()...)
	return blks
}
