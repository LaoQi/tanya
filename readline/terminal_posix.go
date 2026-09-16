//go:build linux || darwin

package readline

import (
	"os"

	"github.com/LaoQi/tanya/ctty"
	"golang.org/x/sys/unix"
)

type unixTerminal struct {
	in    *os.File
	saved ctty.Termios
	keys  keySource
}

func openTerminal() (Terminal, error) {
	return openTerminalFile(os.Stdin)
}

func openTerminalFile(in *os.File) (*unixTerminal, error) {
	t := &unixTerminal{in: in}
	if _, err := ctty.GetTermios(int(in.Fd())); err != nil {
		return nil, err
	}
	t.keys.src = t
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
	t.keys.reset()
	return nil
}

func (t *unixTerminal) Restore() {
	_ = ctty.SetTermios(int(t.in.Fd()), t.saved)
}

func (t *unixTerminal) Size() (Size, bool) {
	cols, rows, ok := ctty.Size(int(os.Stdout.Fd()))
	if !ok {
		return Size{}, false
	}
	return Size{Cols: cols, Rows: rows}, true
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

func (t *unixTerminal) ReadKey() (KeyEvent, error) { return t.keys.readKey() }
