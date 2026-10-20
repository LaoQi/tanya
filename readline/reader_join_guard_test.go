package readline

import (
	"sync"
	"testing"
	"time"
)

type stuckDevice struct {
	mu      sync.Mutex
	raw     int
	rst     int
	gate    chan struct{}
	entered chan struct{}
	once    sync.Once
}

func newStuckDevice() *stuckDevice {
	return &stuckDevice{gate: make(chan struct{}), entered: make(chan struct{})}
}

func (d *stuckDevice) Raw() error { return d.ReaderRaw() }

func (d *stuckDevice) ReaderRaw() error {
	d.mu.Lock()
	d.raw++
	d.mu.Unlock()
	return nil
}

func (d *stuckDevice) Restore() {
	d.mu.Lock()
	d.rst++
	d.mu.Unlock()
}

func (d *stuckDevice) Sane() {}

func (d *stuckDevice) Size() (Size, bool) { return Size{Cols: 80, Rows: 24}, true }

func (d *stuckDevice) backgroundRead() bool { return true }

func (d *stuckDevice) readEvent() (Event, error) {
	d.once.Do(func() { close(d.entered) })
	<-d.gate
	return Event{}, nil
}

func (d *stuckDevice) counts() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.raw, d.rst
}

func TestLendStdinBoundedWhenReaderStuck(t *testing.T) {
	dev := newStuckDevice()
	con := newConsole(dev)
	cancel := con.SubscribeKeys(func(Event) {})
	defer cancel()
	waitFor(t, "常驻读者就位", func() bool {
		raw, _ := dev.counts()
		return raw == 1
	})
	select {
	case <-dev.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("读者未进入读取")
	}

	done := make(chan error, 1)
	go func() {
		lease, err := con.LendStdin()
		if err == nil && lease != nil {
			lease.Release()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("LendStdin 应返回租约: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("读者卡死时 LendStdin 仍无限等待")
	}
	con.mu.Lock()
	broken := con.broken
	con.mu.Unlock()
	if !broken {
		t.Error("放弃 join 后应置 broken，避免与孤儿读者并存")
	}
	if _, rst := dev.counts(); rst != 0 {
		t.Errorf("放弃 join 不应复原终端模式（读者可能仍在读）: rst=%d", rst)
	}
	close(dev.gate)
}
