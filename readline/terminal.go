package readline

import (
	"bufio"
	"errors"
	"os"
	"strings"

	"github.com/LaoQi/tanya/ctty"
)

var ErrUnsupported = errors.New("input: raw mode unsupported on this platform")

var ErrExited = errors.New("input: exit requested")

var exitRequested = func() bool { return ctty.Exiting() }

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

func NewTerminal() (Terminal, bool) {
	if t, err := openTerminal(); err == nil {
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
	if exitRequested() {
		return KeyEvent{}, ErrExited
	}
	line, err := d.r.ReadString('\n')
	if err != nil {
		return KeyEvent{}, err
	}
	return KeyEvent{Code: KeyLine, Text: strings.TrimRight(line, "\r\n")}, nil
}
