package readline

import (
	"errors"
	"io"
	"os/exec"
	"sync"

	"github.com/LaoQi/tanya/ctty"
)

var ErrUnsupported = errors.New("input: raw mode unsupported on this platform")

var ErrExited = errors.New("input: exit requested")

var exitRequested = func() bool { return ctty.Exiting() }

type Size struct {
	Cols int
	Rows int
}

type EventKind uint8

const (
	EventKey EventKind = iota
	EventInterrupt
	EventResize
	EventHangup
	EventIdle
)

type Event struct {
	Kind EventKind
	Key  KeyEvent
}

type Console interface {
	BeginRead() error
	EndRead()
	Sane()
	ReadEvent() (Event, error)
	Subscribe(fn func(Event)) (cancel func())
	Size() (Size, bool)
	LendStdin() (Lease, error)
	LendFull(cmd *exec.Cmd, capture io.Writer) (Lease, error)
}

type device interface {
	Raw() error
	Restore()
	Sane()
	readEvent() (Event, error)
	Size() (Size, bool)
}

func NewConsole() Console {
	d, err := openTerminal()
	if err != nil {
		d = newPipeDevice()
	}
	c := newConsole(d)
	go c.watchSignals()
	return c
}

type consoleImpl struct {
	dev       device
	interrupt <-chan struct{}
	mu        sync.Mutex
	subs      []func(Event)
	pending   bool
}

func newConsole(dev device) *consoleImpl {
	return &consoleImpl{dev: dev, interrupt: ctty.Interrupted()}
}

func (c *consoleImpl) signalInterrupt() {
	c.mu.Lock()
	c.pending = len(c.subs) == 0
	fns := make([]func(Event), len(c.subs))
	copy(fns, c.subs)
	c.mu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn(Event{Kind: EventInterrupt})
		}
	}
}

func (c *consoleImpl) watchSignals() {
	ch := c.interrupt
	for {
		<-ch
		c.signalInterrupt()
		if ctty.Exiting() {
			return
		}
		ch = ctty.Interrupted()
	}
}

func (c *consoleImpl) BeginRead() error { return c.dev.Raw() }

func (c *consoleImpl) EndRead() { c.dev.Restore() }

func (c *consoleImpl) Sane() { c.dev.Sane() }

func (c *consoleImpl) Size() (Size, bool) { return c.dev.Size() }

func (c *consoleImpl) ReadEvent() (Event, error) {
	ev, err := c.dev.readEvent()
	if err == errIdle {
		return Event{Kind: EventIdle}, nil
	}
	if err == nil {
		c.dispatch(ev)
	}
	return ev, err
}

func (c *consoleImpl) Subscribe(fn func(Event)) (cancel func()) {
	c.mu.Lock()
	c.subs = append(c.subs, fn)
	i := len(c.subs) - 1
	replay := c.pending
	c.pending = false
	c.mu.Unlock()
	if replay {
		fn(Event{Kind: EventInterrupt})
	}
	return func() {
		c.mu.Lock()
		if i < len(c.subs) {
			c.subs[i] = nil
		}
		c.mu.Unlock()
	}
}

func (c *consoleImpl) dispatch(ev Event) {
	c.mu.Lock()
	fns := make([]func(Event), len(c.subs))
	copy(fns, c.subs)
	c.mu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn(ev)
		}
	}
}
