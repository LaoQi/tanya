package repl

import (
	"context"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
)

type turnInKind uint8

const (
	inAgent turnInKind = iota + 1
	inDone
)

type turnIn struct {
	kind turnInKind
	ev   agent.Event
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

func (r *REPL) runTurn(ctx context.Context, in agent.Content, t *turn) error {
	ch := make(chan turnIn, 64)
	sink := agent.Sinks(sinkWrap{ch: ch, ctx: ctx}.Emit, notifySink{r: r}.Emit)
	go func() {
		defer close(ch)
		err := r.agent.AskContent(ctx, in, sink)
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
			case inDone:
				return in.err
			}
		case k := <-keys:
			t.Hotkey(k)
		}
	}
}
