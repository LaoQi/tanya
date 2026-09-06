package repl

import (
	"io"
	"testing"
	"time"

	"github.com/LaoQi/tanyan/readline"
)

type fakeWatchTerm struct {
	events   chan readline.KeyEvent
	watchRaw bool
	restored bool
}

func (f *fakeWatchTerm) Raw() error { return nil }
func (f *fakeWatchTerm) Restore()   { f.restored = true }
func (f *fakeWatchTerm) Size() (readline.Size, bool) {
	return readline.Size{}, false
}

func (f *fakeWatchTerm) ReadKey() (readline.KeyEvent, error) {
	return readline.KeyEvent{}, io.EOF
}

func (f *fakeWatchTerm) WatchRaw() error {
	f.watchRaw = true
	return nil
}

func (f *fakeWatchTerm) ReadKeyUntil(stop <-chan struct{}) (readline.KeyEvent, error) {
	select {
	case ev := <-f.events:
		return ev, nil
	case <-stop:
		return readline.KeyEvent{}, readline.ErrWatchStopped
	}
}

func TestREPLInterruptContextCtrlC(t *testing.T) {
	term := &fakeWatchTerm{events: make(chan readline.KeyEvent, 1)}
	r := &REPL{term: term, raw: true}
	ctx, done := r.interruptContext()
	term.events <- readline.KeyEvent{Code: readline.KeyCtrlC}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("watcher 未取消 ctx")
	}
	done()
	if !term.watchRaw {
		t.Fatal("未进入 WatchRaw")
	}
	if !term.restored {
		t.Fatal("done 未 Restore")
	}
}

func TestREPLInterruptContextDoneWithoutKey(t *testing.T) {
	term := &fakeWatchTerm{events: make(chan readline.KeyEvent)}
	r := &REPL{term: term, raw: true}
	ctx, done := r.interruptContext()
	done()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("done() 应取消 ctx")
	}
	if !term.restored {
		t.Fatal("done 未 Restore")
	}
}

func TestREPLInterruptContextDegraded(t *testing.T) {
	term := &fakeWatchTerm{events: make(chan readline.KeyEvent)}
	r := &REPL{term: term, raw: false}
	ctx, done := r.interruptContext()
	done()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("done() 应取消 ctx")
	}
	if term.watchRaw {
		t.Fatal("退化模式不应进入 WatchRaw")
	}
	if term.restored {
		t.Fatal("退化模式不应 Restore")
	}
}

func TestInterruptContextDoneWithoutSignal(t *testing.T) {
	ctx, done := InterruptContext()
	done()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("done() 应取消 ctx")
	}
}
