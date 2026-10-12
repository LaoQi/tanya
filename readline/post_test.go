package readline

import (
	"bytes"
	"errors"
	"testing"
)

func TestPostInjectsEventAndNotifiesSubscribers(t *testing.T) {
	dev := &fakeDevice{err: errIdle}
	c := newConsole(dev)
	if !c.Attachable() {
		t.Fatal("fakeDevice 应可注入")
	}
	seen := 0
	cancel := c.Subscribe(func(ev Event) {
		if ev.Kind == EventTakeover {
			seen++
		}
	})
	defer cancel()
	if !c.Post(EventTakeover) {
		t.Fatal("Post 应成功")
	}
	if seen != 1 {
		t.Fatalf("注入应同步通知订阅者一次: %d", seen)
	}
	ev, err := c.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent: %v", err)
	}
	if ev.Kind != EventTakeover {
		t.Fatalf("应取到注入事件: %+v", ev)
	}
	if ev, err = c.ReadEvent(); err != nil || ev.Kind != EventIdle {
		t.Fatalf("注入取走后应回到设备读取: %+v %v", ev, err)
	}
}

func TestPostUnsupportedOnPipeDevice(t *testing.T) {
	c := newConsole(&pipeDevice{})
	if c.Attachable() {
		t.Fatal("管道设备不应可注入")
	}
	if c.Post(EventTakeover) {
		t.Fatal("管道设备不应入队")
	}
}

type takeoverTerm struct{ fakeTerm }

func (f *takeoverTerm) ReadEvent() (Event, error) { return Event{Kind: EventTakeover}, nil }

func TestEditorTakeoverReturns(t *testing.T) {
	out := &bytes.Buffer{}
	f := &takeoverTerm{}
	f.out = out
	ed := NewEditor(f)
	ed.SetOutput(out)
	_, err := ed.Readline("> ")
	if !errors.Is(err, ErrTakeover) {
		t.Fatalf("期望 ErrTakeover: %v", err)
	}
	if f.raw {
		t.Fatal("Readline 返回后应复原终端模式")
	}
}

func TestDrainInjectedRemovesOnlyKind(t *testing.T) {
	dev := &fakeDevice{err: errIdle}
	c := newConsole(dev)
	if !c.Post(EventInterrupt) || !c.Post(EventTakeover) || !c.Post(EventInterrupt) {
		t.Fatal("Post 应成功")
	}
	c.DrainInjected(EventInterrupt)
	ev, err := c.ReadEvent()
	if err != nil || ev.Kind != EventTakeover {
		t.Fatalf("中断应被清掉、只剩接管事件: %+v %v", ev, err)
	}
	if ev, err = c.ReadEvent(); err != nil || ev.Kind != EventIdle {
		t.Fatalf("接管事件取走后应回到设备读取: %+v %v", ev, err)
	}
}

func TestDrainInjectedOnEmptyQueue(t *testing.T) {
	dev := &fakeDevice{err: errIdle}
	c := newConsole(dev)
	c.DrainInjected(EventInterrupt)
	if !c.Post(EventInterrupt) {
		t.Fatal("Post 应成功")
	}
	if ev, err := c.ReadEvent(); err != nil || ev.Kind != EventInterrupt {
		t.Fatalf("空队列 drain 不影响后续注入: %+v %v", ev, err)
	}
}
