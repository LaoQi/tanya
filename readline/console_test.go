package readline

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type fakeDevice struct {
	events []Event
	err    error
	raw    int
	rst    int
	sane   int
}

func (d *fakeDevice) Raw() error { d.raw++; return nil }
func (d *fakeDevice) Restore()   { d.rst++ }
func (d *fakeDevice) Sane()      { d.sane++ }

func (d *fakeDevice) Size() (Size, bool) { return Size{Cols: 80, Rows: 24}, true }

func (d *fakeDevice) readEvent() (Event, error) {
	if d.err != nil {
		return Event{}, d.err
	}
	if len(d.events) == 0 {
		return Event{}, io.EOF
	}
	ev := d.events[0]
	d.events = d.events[1:]
	return ev, nil
}

func TestConsoleReadEventIdleNoDispatch(t *testing.T) {
	dev := &fakeDevice{err: errIdle}
	c := newConsole(dev)
	got := 0
	cancel := c.Subscribe(func(ev Event) {
		if ev.Kind == EventIdle {
			got++
		}
	})
	defer cancel()
	ev, err := c.ReadEvent()
	if err != nil {
		t.Fatalf("errIdle 应转换为空事件而非错误: %v", err)
	}
	if ev.Kind != EventIdle {
		t.Fatalf("期望 EventIdle: %+v", ev)
	}
	if got != 0 {
		t.Fatal("EventIdle 不应推送给订阅者")
	}
}

func TestConsoleReadEventDispatchesToSubscribers(t *testing.T) {
	dev := &fakeDevice{events: []Event{
		{Kind: EventKey, Key: KeyEvent{Code: KeyRune, Rune: 'a'}},
		{Kind: EventInterrupt},
	}}
	con := newConsole(dev)
	var got []EventKind
	cancel := con.Subscribe(func(ev Event) { got = append(got, ev.Kind) })
	if _, err := con.ReadEvent(); err != nil {
		t.Fatal(err)
	}
	if _, err := con.ReadEvent(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != EventKey || got[1] != EventInterrupt {
		t.Fatalf("订阅者应收全部事件: %v", got)
	}
	cancel()
}

func TestConsoleSubscribeCancel(t *testing.T) {
	dev := &fakeDevice{events: []Event{{Kind: EventKey}, {Kind: EventKey}}}
	con := newConsole(dev)
	n := 0
	cancel := con.Subscribe(func(Event) { n++ })
	if _, err := con.ReadEvent(); err != nil {
		t.Fatal(err)
	}
	cancel()
	cancel()
	if _, err := con.ReadEvent(); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("退订后不应再收到事件，实际 %d", n)
	}
}

func TestConsoleReadEventErrorNotDispatched(t *testing.T) {
	want := errors.New("boom")
	con := newConsole(&fakeDevice{err: want})
	con.Subscribe(func(Event) { t.Fatal("读失败不应分发") })
	if _, err := con.ReadEvent(); !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestConsoleBeginReadForwardsToDevice(t *testing.T) {
	dev := &fakeDevice{}
	con := newConsole(dev)
	if err := con.BeginRead(); err != nil {
		t.Fatal(err)
	}
	con.EndRead()
	if dev.raw != 1 || dev.rst != 1 {
		t.Fatalf("BeginRead/EndRead 应转发设备，raw=%d rst=%d", dev.raw, dev.rst)
	}
}

func TestConsoleSaneForwardsToDevice(t *testing.T) {
	dev := &fakeDevice{}
	con := newConsole(dev)
	con.Sane()
	if dev.sane != 1 {
		t.Fatalf("Sane 应转发设备，sane=%d", dev.sane)
	}
}

func TestKeyEventNormalizesCtrlC(t *testing.T) {
	if ev := keyEvent(KeyEvent{Code: KeyCtrlC}); ev.Kind != EventInterrupt {
		t.Fatalf("0x03 应归一为 Interrupt: %+v", ev)
	}
	ev := keyEvent(KeyEvent{Code: KeyRune, Rune: 'x'})
	if ev.Kind != EventKey || ev.Key.Rune != 'x' {
		t.Fatalf("普通键应保留: %+v", ev)
	}
}

func TestPipeDeviceSynthesizesLineEvent(t *testing.T) {
	con := newConsole(&pipeDevice{r: bufio.NewReader(strings.NewReader("/help\n"))})
	ev, err := con.ReadEvent()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != EventKey || ev.Key.Code != KeyLine || ev.Key.Text != "/help" {
		t.Fatalf("管道设备应合成整行 Key 事件: %+v", ev)
	}
	if _, err := con.ReadEvent(); err != io.EOF {
		t.Fatalf("读尽应 EOF: %v", err)
	}
}

func TestConsolePendingInterruptReplaysToNextSubscriber(t *testing.T) {
	con := newConsole(&fakeDevice{})
	con.signalInterrupt()
	got := 0
	con.Subscribe(func(ev Event) {
		if ev.Kind == EventInterrupt {
			got++
		}
	})
	if got != 1 {
		t.Fatalf("无订阅者时的中断应由下一个订阅者补投递，实际 %d", got)
	}
	got2 := 0
	con.Subscribe(func(ev Event) {
		if ev.Kind == EventInterrupt {
			got2++
		}
	})
	if got2 != 0 {
		t.Fatalf("已消费的中断不应重复补投递，实际 %d", got2)
	}
}

func TestConsoleInterruptDispatchesToCurrentSubscribers(t *testing.T) {
	con := newConsole(&fakeDevice{})
	n := 0
	con.Subscribe(func(ev Event) {
		if ev.Kind == EventInterrupt {
			n++
		}
	})
	con.signalInterrupt()
	if n != 1 {
		t.Fatalf("现有订阅者应收到中断，实际 %d", n)
	}
	n2 := 0
	con.Subscribe(func(ev Event) {
		if ev.Kind == EventInterrupt {
			n2++
		}
	})
	if n2 != 0 {
		t.Fatalf("已 dispatch 的中断不应留给后来订阅者，实际 %d", n2)
	}
}

func TestConsoleInterruptConcurrent(t *testing.T) {
	con := newConsole(&fakeDevice{})
	const subs = 8
	hits := make([]int32, subs)
	var wg sync.WaitGroup
	for i := 0; i < subs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			con.Subscribe(func(ev Event) {
				if ev.Kind == EventInterrupt {
					atomic.AddInt32(&hits[i], 1)
				}
			})
		}(i)
	}
	for i := 0; i < 16; i++ {
		con.signalInterrupt()
	}
	wg.Wait()
	var total int32
	for i := range hits {
		total += atomic.LoadInt32(&hits[i])
	}
	if total == 0 {
		t.Fatal("并发中断至少送达一次")
	}
}
