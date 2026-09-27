//go:build linux || darwin

package readline

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestConsoleResizeFromSignal(t *testing.T) {
	dev := &fakeDevice{err: errIdle}
	c := newConsole(dev)
	got := make(chan EventKind, 1)
	cancel := c.Subscribe(func(ev Event) {
		if ev.Kind == EventResize {
			select {
			case got <- ev.Kind:
			default:
			}
		}
	})
	defer cancel()
	if err := unix.Kill(os.Getpid(), unix.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGWINCH 未经 ctty 到达 Console 订阅者（生产接线缺失）")
	}
	ev, err := c.ReadEvent()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != EventResize {
		t.Fatalf("ReadEvent 应返回尺寸事件: %+v", ev)
	}
}
