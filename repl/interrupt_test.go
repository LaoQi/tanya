package repl

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestInterruptContextSignal(t *testing.T) {
	ctx, done := InterruptContext()
	defer done()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		if ctx.Err() != context.Canceled {
			t.Errorf("ctx.Err: %v", ctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGINT 未取消 ctx")
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
