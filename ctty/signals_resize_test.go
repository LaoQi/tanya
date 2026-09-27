//go:build linux || darwin

package ctty

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOnResizeFiresOnSIGWINCH(t *testing.T) {
	got := make(chan struct{}, 1)
	cancel := OnResize(func() {
		select {
		case got <- struct{}{}:
		default:
		}
	})
	defer cancel()
	if err := unix.Kill(os.Getpid(), unix.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGWINCH 未送达 OnResize 订阅者（武装未生效）")
	}
}

func TestOnResizeCancelStopsDelivery(t *testing.T) {
	var n int32
	cancel := OnResize(func() { atomic.AddInt32(&n, 1) })
	cancel()
	if err := unix.Kill(os.Getpid(), unix.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt32(&n); got != 0 {
		t.Fatalf("取消后仍收到 %d 次回调", got)
	}
}
