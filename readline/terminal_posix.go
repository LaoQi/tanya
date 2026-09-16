//go:build linux || darwin

package readline

import (
	"io"
	"os"

	"github.com/LaoQi/tanya/ctty"
	"golang.org/x/sys/unix"
)

type unixTerminal struct {
	in     *os.File
	saved  ctty.Termios
	parser keyParser
	queue  []KeyEvent
}

func newUnixTerminal() (Terminal, error) {
	return newUnixTerminalFile(os.Stdin)
}

func newUnixTerminalFile(in *os.File) (*unixTerminal, error) {
	t := &unixTerminal{in: in}
	if _, err := ctty.GetTermios(int(in.Fd())); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *unixTerminal) Raw() error {
	saved, err := ctty.GetTermios(int(t.in.Fd()))
	if err != nil {
		return err
	}
	t.saved = saved
	raw := saved
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Oflag &^= unix.OPOST
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 1
	if err := ctty.SetTermiosFlush(int(t.in.Fd()), raw); err != nil {
		return err
	}
	t.queue = nil
	t.parser = keyParser{}
	return nil
}

func (t *unixTerminal) Restore() {
	_ = ctty.SetTermios(int(t.in.Fd()), t.saved)
}

func (t *unixTerminal) Size() (Size, bool) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return Size{}, false
	}
	return Size{Cols: int(ws.Col), Rows: int(ws.Row)}, true
}

func (t *unixTerminal) readChunk(p []byte) (int, error) {
	return unix.Read(int(t.in.Fd()), p)
}

func (t *unixTerminal) hungUp() bool {
	fds := []unix.PollFd{{Fd: int32(t.in.Fd()), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, 0)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			return false
		}
		return fds[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0
	}
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
			if err == unix.EIO {
				return KeyEvent{}, io.EOF
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
		if t.hungUp() {
			return KeyEvent{}, io.EOF
		}
	}
	ev := t.queue[0]
	t.queue = t.queue[1:]
	return ev, nil
}
