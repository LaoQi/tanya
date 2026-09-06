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
