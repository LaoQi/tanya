package readline

import (
	"errors"

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
	return newPipeDevice(), false
}
