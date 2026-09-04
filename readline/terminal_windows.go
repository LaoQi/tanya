//go:build windows

package readline

import (
	"errors"
)

type windowsTerminal struct{}

func newUnixTerminal() (Terminal, error) {
	return nil, ErrUnsupported
}

func newWindowsTerminal() (Terminal, error) {
	return nil, errors.New("input: windows terminal backend not implemented yet")
}

func (t *windowsTerminal) Raw() error                 { return ErrUnsupported }
func (t *windowsTerminal) Restore()                   {}
func (t *windowsTerminal) Size() (Size, bool)         { return Size{}, false }
func (t *windowsTerminal) ReadKey() (KeyEvent, error) { return KeyEvent{}, ErrUnsupported }
