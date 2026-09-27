package repl

import (
	"context"
	"sync"

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

func (r *REPL) startHotkeys() (<-chan readline.KeyEvent, func()) {
	if !hotkeysSupported || !r.keys || !r.reasonVisible() {
		return nil, func() {}
	}
	if err := r.con.BeginRead(); err != nil {
		return nil, func() {}
	}
	keys := make(chan readline.KeyEvent)
	stopCh := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		defer r.con.EndRead()
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			ev, err := r.con.ReadEvent()
			if err != nil {
				return
			}
			if ev.Kind != readline.EventKey || ev.Key.Code != readline.KeyCtrlO {
				continue
			}
			select {
			case keys <- ev.Key:
			case <-stopCh:
				return
			}
		}
	}()
	stop := func() {
		once.Do(func() {
			close(stopCh)
			<-done
		})
	}
	return keys, stop
}

func (r *REPL) runTurn(ctx context.Context, q string, t *turn) error {
	ch := make(chan turnIn, 64)
	go func() {
		defer close(ch)
		err := r.agent.Ask(ctx, q, sinkWrap{ch: ch, ctx: ctx}.Emit)
		ch <- turnIn{kind: inDone, err: err}
	}()
	keys, stop := r.startHotkeys()
	defer stop()
	for {
		select {
		case in := <-ch:
			switch in.kind {
			case inAgent:
				if in.ack != nil {
					stop()
					keys = nil
				}
				t.Handle(in.ev)
				if in.ack != nil {
					close(in.ack)
				}
			case inDone:
				return in.err
			}
		case k, ok := <-keys:
			if !ok {
				keys = nil
				continue
			}
			t.Hotkey(k)
		}
	}
}
