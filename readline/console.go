package readline

import (
	"errors"
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
)

type Event struct {
	Kind EventKind
	Key  KeyEvent
}

type Console interface {
	BeginRead() error
	EndRead()
	ReadEvent() (Event, error)
	Subscribe(fn func(Event)) (cancel func())
	Size() (Size, bool)
}

type device interface {
	Raw() error
	Restore()
	readEvent() (Event, error)
	Size() (Size, bool)
}

func NewConsole() Console {
	if d, err := openTerminal(); err == nil {
		return newConsole(d)
	}
	return newConsole(newPipeDevice())
}

type consoleImpl struct {
	dev  device
	mu   sync.Mutex
	subs []func(Event)
}

func newConsole(dev device) *consoleImpl {
	return &consoleImpl{dev: dev}
}

func (c *consoleImpl) BeginRead() error { return c.dev.Raw() }

func (c *consoleImpl) EndRead() { c.dev.Restore() }

func (c *consoleImpl) Size() (Size, bool) { return c.dev.Size() }

func (c *consoleImpl) ReadEvent() (Event, error) {
	ev, err := c.dev.readEvent()
	if err == nil {
		c.dispatch(ev)
	}
	return ev, err
}

func (c *consoleImpl) Subscribe(fn func(Event)) (cancel func()) {
	c.mu.Lock()
	c.subs = append(c.subs, fn)
	i := len(c.subs) - 1
	c.mu.Unlock()
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
