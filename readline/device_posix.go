//go:build linux || darwin

package readline

import (
	"os"

	"github.com/LaoQi/tanya/ctty"
	"golang.org/x/sys/unix"
)

type posixTTY struct {
	in    *os.File
	out   *os.File
	saved ctty.Termios
	keys  keySource
}

func openTerminal() (Terminal, error) {
	return openTerminalFile(os.Stdin, os.Stdout)
}

func openTerminalFile(in, out *os.File) (*posixTTY, error) {
	if os.Getenv("TANYA_NO_RAW_INPUT") != "" {
		return nil, ErrUnsupported
	}
	t := &posixTTY{in: in, out: out}
	if _, err := ctty.GetTermios(int(in.Fd())); err != nil {
		return nil, err
	}
	t.keys.src = t
	return t, nil
}

func (t *posixTTY) Raw() error {
	saved, err := ctty.GetTermios(int(t.in.Fd()))
	if err != nil {
		return err
	}
	t.saved = saved
	if err := ctty.SetTermiosFlush(int(t.in.Fd()), keysTermios(saved)); err != nil {
		return err
	}
	t.keys.reset()
	return nil
}

func keysTermios(saved ctty.Termios) ctty.Termios {
	raw := saved
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Oflag &^= unix.OPOST
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 1
	return raw
}

func saneTermios(t ctty.Termios) ctty.Termios {
	s := t
	s.Iflag |= unix.ICRNL | unix.IXON
	s.Lflag |= unix.ISIG | unix.ICANON | unix.ECHO | unix.IEXTEN
	s.Oflag |= unix.OPOST | unix.ONLCR
	return s
}

func (t *posixTTY) Restore() {
	_ = ctty.SetTermios(int(t.in.Fd()), t.saved)
}

func (t *posixTTY) Size() (Size, bool) {
	if t.out == nil {
		return Size{}, false
	}
	cols, rows, ok := ctty.Size(int(t.out.Fd()))
	if !ok {
		return Size{}, false
	}
	return Size{Cols: cols, Rows: rows}, true
}

func (t *posixTTY) readChunk(p []byte) (int, error) {
	return unix.Read(int(t.in.Fd()), p)
}

func (t *posixTTY) hungUp() bool {
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

func (t *posixTTY) ReadKey() (KeyEvent, error) { return t.keys.readKey() }
