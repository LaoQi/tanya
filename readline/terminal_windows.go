//go:build windows

package readline

import (
	"os"
	"time"
	"unsafe"

	"github.com/LaoQi/tanya/ctty"
	"golang.org/x/sys/windows"
)

const (
	consolePollInterval = 5 * time.Millisecond
	consoleReadTimeout  = time.Second
)

var (
	kernel32                          = windows.NewLazySystemDLL("kernel32.dll")
	procGetNumberOfConsoleInputEvents = kernel32.NewProc("GetNumberOfConsoleInputEvents")
)

type windowsTerminal struct {
	in    *os.File
	out   *os.File
	saved uint32
	keys  keySource
}

func openTerminal() (Terminal, error) {
	return openTerminalFile(os.Stdin, os.Stdout)
}

func openTerminalFile(in, out *os.File) (*windowsTerminal, error) {
	if os.Getenv("TANYA_NO_RAW_INPUT") != "" {
		return nil, ErrUnsupported
	}
	saved, ok := ctty.ConsoleMode(int(in.Fd()))
	if !ok {
		return nil, ErrUnsupported
	}
	t := &windowsTerminal{in: in, out: out, saved: saved}
	t.keys.src = t
	return t, nil
}

func (t *windowsTerminal) Raw() error {
	mode, ok := ctty.ConsoleMode(int(t.in.Fd()))
	if !ok {
		return ErrUnsupported
	}
	t.saved = mode
	raw := mode | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	raw &^= windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT
	if !ctty.SetConsoleMode(int(t.in.Fd()), raw) {
		return ErrUnsupported
	}
	ctty.EnableVT(int(t.out.Fd()))
	t.keys.reset()
	return nil
}

func (t *windowsTerminal) Restore() {
	ctty.SetConsoleMode(int(t.in.Fd()), t.saved)
}

func (t *windowsTerminal) Size() (Size, bool) {
	cols, rows, ok := ctty.Size(int(t.out.Fd()))
	if !ok {
		return Size{}, false
	}
	return Size{Cols: cols, Rows: rows}, true
}

func (t *windowsTerminal) readChunk(p []byte) (int, error) {
	deadline := time.Now().Add(consoleReadTimeout)
	for {
		if n, ok := consoleInputEvents(int(t.in.Fd())); ok && n > 0 {
			var done uint32
			if err := windows.ReadFile(windows.Handle(t.in.Fd()), p, &done, nil); err != nil {
				return 0, err
			}
			return int(done), nil
		}
		if !time.Now().Before(deadline) {
			return 0, nil
		}
		time.Sleep(consolePollInterval)
	}
}

func (t *windowsTerminal) ReadKey() (KeyEvent, error) { return t.keys.readKey() }

func consoleInputEvents(fd int) (int, bool) {
	var n uint32
	r, _, _ := procGetNumberOfConsoleInputEvents.Call(uintptr(windows.Handle(fd)), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return 0, false
	}
	return int(n), true
}
