package readline

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

var ErrUnsupported = errors.New("input: raw mode unsupported on this platform")

var ErrWatchStopped = errors.New("input: key watch stopped")

type Size struct {
	Cols int
	Rows int
}

type Terminal interface {
	Raw() error
	Restore()
	ReadKey() (KeyEvent, error)
	Size() (Size, bool)
}

type KeyWatcher interface {
	WatchRaw() error
	ReadKeyUntil(stop <-chan struct{}) (KeyEvent, error)
}

func NewTerminal() (Terminal, bool) {
	if t, err := newUnixTerminal(); err == nil {
		return t, true
	}
	return NewDegraded(), false
}

type Degraded struct {
	r *bufio.Reader
}

func NewDegraded() *Degraded {
	return &Degraded{r: bufio.NewReader(os.Stdin)}
}

func (d *Degraded) Raw() error { return ErrUnsupported }

func (d *Degraded) Restore() {}

func (d *Degraded) Size() (Size, bool) { return Size{}, false }

func (d *Degraded) ReadKey() (KeyEvent, error) {
	line, err := d.r.ReadString('\n')
	if err != nil {
		return KeyEvent{}, err
	}
	return KeyEvent{Code: KeyLine, Text: strings.TrimRight(line, "\r\n")}, nil
}
