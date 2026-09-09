package repl

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSpinnerNonTTYNoop(t *testing.T) {
	var mu sync.Mutex
	sp := newSpinner(&mu, false)
	sp.start(spinWaiting)
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
	sp.start(spinWaiting)
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

func TestSpinLine(t *testing.T) {
	if got := spinLine(spinWaiting, 2*time.Second, "⠋"); !strings.Contains(got, "等待响应") || !strings.Contains(got, "2s") {
		t.Errorf("等待文案: %q", got)
	}
	if got := spinLine(spinThinking, 3*time.Second, "⠙"); !strings.Contains(got, "思考中") || !strings.Contains(got, "3s") {
		t.Errorf("思考文案: %q", got)
	}
	if got := spinLine(spinRunning, time.Second, "⠹"); !strings.Contains(got, "执行中") {
		t.Errorf("执行文案: %q", got)
	}
}
