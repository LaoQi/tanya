package repl

import (
	"errors"
	"fmt"
	"github.com/LaoQi/tanyan/render"
	"github.com/LaoQi/tanyan/render/ir"
	"github.com/LaoQi/tanyan/render/markdown"
	"github.com/LaoQi/tanyan/render/term"
	"github.com/LaoQi/tanyan/render/theme"
	"time"

	"github.com/LaoQi/tanyan/agent"
)

// Kind 标记每次输出的类别，是噪音门禁与测试断言的把手（不导出包外、不进 agent.Event）。
type Kind uint8

const (
	KindContent    Kind = iota + 1 // assistant 正文（流式 + 回放）
	KindReasoning                  // 思维链（当前不上屏，预留）
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
}

func (r *REPL) beginTurn(done func()) *turn {
	return &turn{
		r:     r,
		start: time.Now(),
		done:  done,
		f: &flow{
			st:   r.st,
			prof: r.prof,
			sem:  r.sem,
			md:   markdown.NewMarkdownBuf(),
			rend: r.rend,
		},
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
	case agent.EventContent:
		t.writeContent(e.Text)
	case agent.EventToolStart, agent.EventResponse:
		t.settleMd()
		t.r.view.Handle(e)
	default:
		t.r.view.Handle(e)
	}
}

func (t *turn) writeContent(s string) {
	if !t.f.mdEnabled() {
		t.r.print(s, KindContent)
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
	t.settleMd()
	if t.done != nil {
		t.done()
	}
	if err != nil {
		var ie *agent.InterruptError
		if errors.As(err, &ie) {
			if ie.Kept {
				t.f.st.err.emit(KindError, MsgInterruptKept)
			} else {
				t.f.st.err.emit(KindError, MsgInterruptBare)
			}
		} else {
			t.f.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", err))
		}
	}
	t.f.emit(KindDecor, turnSep(t.f.prof, t.f.sem, dur))
}

// mdBlocks 把整段文本按 markdown 管线解析为块（回放等一次性展示用，不复用回合缓冲）。
func mdBlocks(text string) []ir.Block {
	buf := markdown.NewMarkdownBuf()
	var blks []ir.Block
	blks = append(blks, buf.Write(text)...)
	blks = append(blks, buf.Close()...)
	return blks
}
