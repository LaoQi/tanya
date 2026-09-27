package readline

import (
	"io"
	"sync"
	"testing"
	"time"
)

type scriptDevice struct {
	mu   sync.Mutex
	raw  int
	rst  int
	fail error
	ch   chan Event
}

func newScriptDevice() *scriptDevice { return &scriptDevice{ch: make(chan Event, 16)} }

func (d *scriptDevice) Raw() error {
	d.mu.Lock()
	d.raw++
	d.mu.Unlock()
	return nil
}

func (d *scriptDevice) ReaderRaw() error { return d.Raw() }

func (d *scriptDevice) Restore() {
	d.mu.Lock()
	d.rst++
	d.mu.Unlock()
}

func (d *scriptDevice) Sane() {}

func (d *scriptDevice) Size() (Size, bool) { return Size{Cols: 80, Rows: 24}, true }

func (d *scriptDevice) backgroundRead() bool { return true }

func (d *scriptDevice) readEvent() (Event, error) {
	d.mu.Lock()
	fail := d.fail
	d.mu.Unlock()
	if fail != nil {
		return Event{}, fail
	}
	select {
	case ev := <-d.ch:
		return ev, nil
	case <-time.After(time.Millisecond):
		return Event{}, errIdle
	}
}

func (d *scriptDevice) counts() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.raw, d.rst
}

func (d *scriptDevice) setFail(err error) {
	d.mu.Lock()
	d.fail = err
	d.mu.Unlock()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

func TestConsoleKeysSubscriptionStartsReader(t *testing.T) {
	dev := newScriptDevice()
	con := newConsole(dev)
	keys := make(chan Event, 8)
	cancel := con.SubscribeKeys(func(ev Event) { keys <- ev })
	defer cancel()
	waitFor(t, "按键订阅应起常驻读者并置 keys 模式", func() bool {
		raw, _ := dev.counts()
		return raw == 1
	})
	dev.ch <- Event{Kind: EventKey, Key: KeyEvent{Code: KeyCtrlO}}
	select {
	case ev := <-keys:
		if ev.Kind != EventKey || ev.Key.Code != KeyCtrlO {
			t.Fatalf("常驻读者应推原样按键: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("常驻读者未推送按键")
	}
	cancel()
	if raw, rst := dev.counts(); raw != 1 || rst != 1 {
		t.Fatalf("取消最后一个按键订阅应停读者并复原终端: raw=%d rst=%d", raw, rst)
	}
}

func TestConsolePlainSubscriptionKeepsCooked(t *testing.T) {
	dev := newScriptDevice()
	con := newConsole(dev)
	cancel := con.Subscribe(func(Event) {})
	defer cancel()
	time.Sleep(20 * time.Millisecond)
	if raw, rst := dev.counts(); raw != 0 || rst != 0 {
		t.Fatalf("普通订阅不应后台读终端: raw=%d rst=%d", raw, rst)
	}
}

func TestConsoleExclusiveReadPreemptsReader(t *testing.T) {
	dev := newScriptDevice()
	con := newConsole(dev)
	cancel := con.SubscribeKeys(func(Event) {})
	defer cancel()
	waitFor(t, "常驻读者启动", func() bool {
		raw, _ := dev.counts()
		return raw == 1
	})
	if err := con.BeginRead(); err != nil {
		t.Fatal(err)
	}
	if raw, rst := dev.counts(); raw != 2 || rst != 1 {
		t.Fatalf("BeginRead 应先停读者并复原终端: raw=%d rst=%d", raw, rst)
	}
	con.EndRead()
	waitFor(t, "EndRead 后常驻读者恢复", func() bool {
		raw, _ := dev.counts()
		return raw == 3
	})
}

func TestConsoleLendSuspendsReader(t *testing.T) {
	dev := newScriptDevice()
	con := newConsole(dev)
	keys := make(chan Event, 8)
	cancel := con.SubscribeKeys(func(ev Event) { keys <- ev })
	defer cancel()
	waitFor(t, "常驻读者启动", func() bool {
		raw, _ := dev.counts()
		return raw == 1
	})
	lease, err := con.LendStdin()
	if err != nil {
		t.Fatal(err)
	}
	if raw, rst := dev.counts(); raw != 1 || rst != 1 {
		t.Fatalf("借出前应先停读者并复原终端: raw=%d rst=%d", raw, rst)
	}
	dev.ch <- Event{Kind: EventKey, Key: KeyEvent{Code: KeyCtrlO}}
	time.Sleep(20 * time.Millisecond)
	select {
	case ev := <-keys:
		t.Fatalf("借出期不应有读者送键: %+v", ev)
	default:
	}
	lease.Release()
	waitFor(t, "租约归还后常驻读者恢复", func() bool {
		raw, _ := dev.counts()
		return raw == 2
	})
	lease.Release()
	dev.ch <- Event{Kind: EventKey, Key: KeyEvent{Code: KeyCtrlO}}
	select {
	case <-keys:
	case <-time.After(2 * time.Second):
		t.Fatal("归还后读者应重新送键")
	}
}

func TestConsoleNoBackgroundReadDevice(t *testing.T) {
	dev := &fakeDevice{err: errIdle}
	con := newConsole(dev)
	cancel := con.SubscribeKeys(func(Event) {})
	defer cancel()
	time.Sleep(20 * time.Millisecond)
	if dev.raw != 0 {
		t.Fatalf("无后台读能力的设备不应起常驻读者: raw=%d", dev.raw)
	}
}

func TestConsoleBrokenReaderNotRestarted(t *testing.T) {
	dev := newScriptDevice()
	dev.setFail(io.EOF)
	con := newConsole(dev)
	cancel := con.SubscribeKeys(func(Event) {})
	defer cancel()
	waitFor(t, "读者因设备 EOF 退出并复原终端", func() bool {
		_, rst := dev.counts()
		return rst == 1
	})
	cancel2 := con.SubscribeKeys(func(Event) {})
	defer cancel2()
	time.Sleep(20 * time.Millisecond)
	if raw, _ := dev.counts(); raw != 1 {
		t.Fatalf("异常退出的读者不应自启: raw=%d", raw)
	}
	dev.setFail(nil)
	if err := con.BeginRead(); err != nil {
		t.Fatal(err)
	}
	con.EndRead()
	waitFor(t, "重开独占会话后常驻读者恢复", func() bool {
		raw, _ := dev.counts()
		return raw == 3
	})
}
