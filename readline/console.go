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
	SubscribeKeys(fn func(Event)) (cancel func())
	Size() (Size, bool)
	LendStdin() (Lease, error)
	LendFull(cmd *exec.Cmd, capture io.Writer) (Lease, error)
}

type device interface {
	Raw() error
	ReaderRaw() error
	Restore()
	Sane()
	readEvent() (Event, error)
	Size() (Size, bool)
}

type backgroundReader interface {
	backgroundRead() bool
}

func backgroundReadable(d device) bool {
	b, ok := d.(backgroundReader)
	return ok && b.backgroundRead()
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

type consoleState uint8

const (
	stateIdle consoleState = iota
	stateExclusive
	stateLent
)

type consoleReader struct {
	stop chan struct{}
	done chan struct{}
}

type consoleSub struct {
	fn  func(Event)
	key bool
}

type consoleImpl struct {
	dev       device
	interrupt <-chan struct{}
	mu        sync.Mutex
	subs      []*consoleSub
	pending   bool

	resizePending bool
	resizeCancel  func()

	state  consoleState
	reader *consoleReader
	broken bool
}

func newConsole(dev device) *consoleImpl {
	c := &consoleImpl{dev: dev, interrupt: ctty.Interrupted()}
	c.resizeCancel = ctty.OnResize(c.signalResize)
	return c
}

func (c *consoleImpl) signalInterrupt() {
	c.mu.Lock()
	c.pending = c.countLocked(false) == 0
	fns := c.callbacksLocked()
	c.mu.Unlock()
	for _, fn := range fns {
		fn(Event{Kind: EventInterrupt})
	}
}

// signalResize 记一次尺寸变更：置位供 ReadEvent 消费（多次缩放合并为一次），并推给订阅者。
// 借出期（stateLent）不推订阅者——终端已归子进程，窗口尺寸由借出方自行传播。
func (c *consoleImpl) signalResize() {
	c.mu.Lock()
	c.resizePending = true
	lent := c.state == stateLent
	fns := c.callbacksLocked()
	c.mu.Unlock()
	if lent {
		return
	}
	for _, fn := range fns {
		fn(Event{Kind: EventResize})
	}
}

func (c *consoleImpl) takeResize() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.resizePending {
		return false
	}
	c.resizePending = false
	return true
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

func (c *consoleImpl) BeginRead() error {
	c.parkReader()
	if err := c.dev.Raw(); err != nil {
		return err
	}
	c.mu.Lock()
	c.state = stateExclusive
	c.broken = false
	c.mu.Unlock()
	return nil
}

func (c *consoleImpl) EndRead() {
	c.dev.Restore()
	c.mu.Lock()
	if c.state == stateExclusive {
		c.state = stateIdle
	}
	c.maybeStartReaderLocked()
	c.mu.Unlock()
}

func (c *consoleImpl) Sane() { c.dev.Sane() }

func (c *consoleImpl) Size() (Size, bool) { return c.dev.Size() }

func (c *consoleImpl) ReadEvent() (Event, error) {
	if c.takeResize() {
		return Event{Kind: EventResize}, nil
	}
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
	return c.subscribe(fn, false)
}

func (c *consoleImpl) SubscribeKeys(fn func(Event)) (cancel func()) {
	return c.subscribe(fn, true)
}

func (c *consoleImpl) subscribe(fn func(Event), keys bool) func() {
	s := &consoleSub{fn: fn, key: keys}
	c.mu.Lock()
	c.subs = append(c.subs, s)
	replay := c.pending
	c.pending = false
	c.maybeStartReaderLocked()
	c.mu.Unlock()
	if replay {
		fn(Event{Kind: EventInterrupt})
	}
	return func() {
		c.mu.Lock()
		for i, got := range c.subs {
			if got == s {
				c.subs[i] = nil
				break
			}
		}
		c.mu.Unlock()
		if keys {
			c.unparkReader()
		}
	}
}

func (c *consoleImpl) dispatch(ev Event) {
	c.mu.Lock()
	fns := c.callbacksLocked()
	c.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

func (c *consoleImpl) callbacksLocked() []func(Event) {
	out := make([]func(Event), 0, len(c.subs))
	for _, s := range c.subs {
		if s != nil {
			out = append(out, s.fn)
		}
	}
	return out
}

func (c *consoleImpl) countLocked(keysOnly bool) int {
	n := 0
	for _, s := range c.subs {
		if s == nil {
			continue
		}
		if keysOnly && !s.key {
			continue
		}
		n++
	}
	return n
}

func (c *consoleImpl) maybeStartReaderLocked() {
	if c.reader != nil || c.broken || c.state != stateIdle {
		return
	}
	if c.countLocked(true) == 0 || !backgroundReadable(c.dev) {
		return
	}
	if err := c.dev.ReaderRaw(); err != nil {
		return
	}
	r := &consoleReader{stop: make(chan struct{}), done: make(chan struct{})}
	c.reader = r
	go c.readLoop(r)
}

func (c *consoleImpl) readLoop(r *consoleReader) {
	defer close(r.done)
	defer func() {
		c.mu.Lock()
		owner := c.reader == r
		if owner {
			c.reader = nil
			c.broken = true
		}
		c.mu.Unlock()
		if owner {
			c.dev.Restore()
		}
	}()
	for {
		select {
		case <-r.stop:
			return
		default:
		}
		if c.takeResize() {
			c.dispatch(Event{Kind: EventResize})
			continue
		}
		ev, err := c.dev.readEvent()
		if err == errIdle {
			continue
		}
		if err != nil {
			return
		}
		select {
		case <-r.stop:
			return
		default:
		}
		c.dispatch(ev)
	}
}

func (c *consoleImpl) parkReader() {
	c.mu.Lock()
	r := c.detachReaderLocked()
	c.mu.Unlock()
	c.joinReader(r)
}

func (c *consoleImpl) unparkReader() {
	c.mu.Lock()
	if c.countLocked(true) > 0 {
		c.mu.Unlock()
		return
	}
	r := c.detachReaderLocked()
	c.mu.Unlock()
	c.joinReader(r)
}

func (c *consoleImpl) detachReaderLocked() *consoleReader {
	r := c.reader
	if r == nil {
		return nil
	}
	c.reader = nil
	close(r.stop)
	return r
}

func (c *consoleImpl) joinReader(r *consoleReader) {
	if r == nil {
		return
	}
	<-r.done
	c.dev.Restore()
}

func (c *consoleImpl) beginLend() {
	c.mu.Lock()
	c.state = stateLent
	c.mu.Unlock()
	c.parkReader()
}

func (c *consoleImpl) endLend() {
	c.mu.Lock()
	if c.state == stateLent {
		c.state = stateIdle
	}
	c.maybeStartReaderLocked()
	c.mu.Unlock()
}

type parkedLease struct {
	Lease
	c    *consoleImpl
	once sync.Once
}

func (l *parkedLease) Release() {
	l.once.Do(func() {
		l.Lease.Release()
		l.c.endLend()
	})
}

func (c *consoleImpl) LendStdin() (Lease, error) {
	c.beginLend()
	l, err := newStdinLease()
	if err != nil {
		c.endLend()
		return nil, err
	}
	return &parkedLease{Lease: l, c: c}, nil
}

func (c *consoleImpl) LendFull(cmd *exec.Cmd, capture io.Writer) (Lease, error) {
	c.beginLend()
	l, err := lendFullImpl(cmd, capture)
	if err != nil {
		c.endLend()
		return nil, err
	}
	return &parkedLease{Lease: l, c: c}, nil
}
