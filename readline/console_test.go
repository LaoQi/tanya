package readline

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeDevice struct {
	events []Event
	err    error
	raw    int
	rst    int
}

func (d *fakeDevice) Raw() error         { d.raw++; return nil }
func (d *fakeDevice) Restore()           { d.rst++ }
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
