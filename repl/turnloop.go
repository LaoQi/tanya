package repl

import (
	"context"

	"github.com/LaoQi/battlelab/agent"
	"github.com/LaoQi/tanya/readline"
)

type turnInKind uint8

const (
	inAgent turnInKind = iota + 1
	inHint
	inDone
)

// hintLine 是钩子在回合内投来的一行：屏幕提示（isErr 假）或注入失败的错误行。
type hintLine struct {
	text  string
	isErr bool
}

type turnIn struct {
	kind turnInKind
	ev   agent.Event
	hint hintLine
	ack  chan struct{}
	err  error
}

type sinkWrap struct {
	ch  chan turnIn
	ctx context.Context
}

func (w sinkWrap) Emit(e agent.Event) {
	var ack chan struct{}
	if e.Kind == agent.EventToolStart {
		ack = make(chan struct{})
	}
	select {
	case w.ch <- turnIn{kind: inAgent, ev: e, ack: ack}:
	case <-w.ctx.Done():
		return
	}
	if ack != nil {
		select {
		case <-ack:
		case <-w.ctx.Done():
		}
	}
}

const hotkeyQueue = 8

func (r *REPL) startHotkeys() (<-chan readline.KeyEvent, func()) {
	if !hotkeysSupported || !r.keys || !r.reasonVisible() {
		return nil, func() {}
	}
	keys := make(chan readline.KeyEvent, hotkeyQueue)
	cancel := r.con.SubscribeKeys(func(ev readline.Event) {
		if ev.Kind != readline.EventKey || ev.Key.Code != readline.KeyCtrlO {
			return
		}
		select {
		case keys <- ev.Key:
		default:
		}
	})
	return keys, cancel
}

func (r *REPL) runTurn(ctx context.Context, in string, t *turn, cont bool) error {
	ch := make(chan turnIn, 64)
	if r.hint != nil {
		// 钩子在 agent 的回合 goroutine 上跑，渲染只在 REPL goroutine（心跳状态行同处），
		// 故提示行经本回合的通道排队——顺序落在 EventRequestStart 之后、请求构造之前。
		r.hint.render(func(text string, isErr bool) bool {
			ack := make(chan struct{})
			select {
			case ch <- turnIn{kind: inHint, hint: hintLine{text: text, isErr: isErr}, ack: ack}:
			case <-ctx.Done():
				return false
			}
			select {
			case <-ack:
			case <-ctx.Done():
			}
			return true
		})
		defer r.hint.render(nil)
	}
	sink := agent.Sinks(sinkWrap{ch: ch, ctx: ctx}.Emit, notifySink{r: r}.Emit)
	go func() {
		defer close(ch)
		var err error
		if cont {
			err = r.agent.Continue(ctx, sink)
		} else {
			err = r.agent.Ask(ctx, in, sink)
		}
		ch <- turnIn{kind: inDone, err: err}
	}()
	keys, cancel := r.startHotkeys()
	defer cancel()
	for {
		select {
		case in := <-ch:
			switch in.kind {
			case inAgent:
				t.Handle(in.ev)
				if in.ack != nil {
					close(in.ack)
				}
			case inHint:
				t.hint(in.hint)
				if in.ack != nil {
					close(in.ack)
				}
			case inDone:
				return in.err
			}
		case k := <-keys:
			t.Hotkey(k)
		}
	}
}
