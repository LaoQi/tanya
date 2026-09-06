package repl

import (
	"sync"
	"testing"
	"time"
)

func TestSpinnerNonTTYNoop(t *testing.T) {
	var mu sync.Mutex
	sp := newSpinner(&mu, false)
	sp.start(func(elapsed time.Duration, frame string) string {
		return frame + " 等待响应"
	})
	time.Sleep(250 * time.Millisecond)
	sp.stop()
	if sp.active {
		t.Error("非 TTY 下 spinner 不应激活")
	}
}

func TestSpinElapsed(t *testing.T) {
	if got := spinElapsed(2500 * time.Millisecond); got != "2s" {
		t.Errorf("got %q", got)
	}
	if got := spinElapsed(900 * time.Millisecond); got != "0s" {
		t.Errorf("got %q", got)
	}
}

type blockedWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (w *blockedWriter) Write(p []byte) (int, error) {
	w.entered <- struct{}{}
	<-w.release
	return len(p), nil
}

func TestSpinnerStopTimeout(t *testing.T) {
	var mu sync.Mutex
	sp := newSpinner(&mu, true)
	bw := &blockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	sp.out = bw
	sp.start(func(elapsed time.Duration, frame string) string {
		return frame + " 等待响应"
	})
	<-bw.entered
	done := make(chan struct{})
	go func() {
		sp.stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop 应在有界超时后返回而不是永久阻塞")
	}
	if sp.active {
		t.Error("超时后 active 应置 false")
	}
	close(bw.release)
}
