//go:build unix && !illumos && !ios

package readline

import (
	"os"

	"golang.org/x/sys/unix"
)

type unixTerminal struct {
	saved  unix.Termios
	parser keyParser
	queue  []KeyEvent
}

func newUnixTerminal() (Terminal, error) {
	t := &unixTerminal{}
	if _, err := getTermios(int(os.Stdin.Fd())); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *unixTerminal) Raw() error {
	saved, err := getTermios(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	t.saved = *saved
	raw := *saved
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Oflag &^= unix.OPOST
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 1
	if err := setTermiosFlush(int(os.Stdin.Fd()), &raw); err != nil {
		return err
	}
	t.queue = nil
	t.parser = keyParser{}
	return nil
}

func (t *unixTerminal) Restore() {
	_ = setTermios(int(os.Stdin.Fd()), &t.saved)
}

func (t *unixTerminal) Size() (Size, bool) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return Size{}, false
	}
	return Size{Cols: int(ws.Col), Rows: int(ws.Row)}, true
}

func (t *unixTerminal) readChunk(p []byte) (int, error) {
	return unix.Read(int(os.Stdin.Fd()), p)
}

func (t *unixTerminal) ReadKey() (KeyEvent, error) {
	if len(t.queue) > 0 {
		ev := t.queue[0]
		t.queue = t.queue[1:]
		return ev, nil
	}
	buf := make([]byte, 256)
	for {
		n, err := t.readChunk(buf)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return KeyEvent{}, err
		}
		if n > 0 {
			t.queue = append(t.queue, t.parser.feed(buf[:n])...)
		}
		if t.parser.needsMore() && n > 0 {
			continue
		}
		if t.parser.needsMore() && n == 0 {
			t.queue = append(t.queue, t.parser.flush()...)
			break
		}
		if len(t.queue) > 0 {
			break
		}
	}
	ev := t.queue[0]
	t.queue = t.queue[1:]
	return ev, nil
}
