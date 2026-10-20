//go:build linux || darwin

package readline

import (
	"os"

	"github.com/LaoQi/tanya/ctty"
	"golang.org/x/sys/unix"
)

type posixTTY struct {
	in         *os.File
	out        *os.File
	saved      ctty.Termios
	readerMode ctty.Termios
	keys       keySource
}

func openTerminal() (device, error) {
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

func (t *posixTTY) ReaderRaw() error {
	saved, err := ctty.GetTermios(int(t.in.Fd()))
	if err != nil {
		return err
	}
	t.saved = saved
	if err := ctty.SetTermiosFlush(int(t.in.Fd()), readerTermios(saved)); err != nil {
		return err
	}
	t.keys.reset()
	t.readerMode = readerTermios(saved)
	return nil
}

// ReassertReader 核对常驻读者的模式位是否仍是自己设下的形态：被外部程序（子进程经 /dev/tty 改 termios）
// 打回行缓冲时重设回去，避免按键被行缓冲吞掉（读者本身靠轮询读，不受影响）。
// 有意不改 t.saved：退出时的复原基线仍是进入读者前的原始值。
func (t *posixTTY) ReassertReader() error {
	fd := int(t.in.Fd())
	cur, err := ctty.GetTermios(fd)
	if err != nil {
		return err
	}
	want := t.readerMode
	if cur.Cc[unix.VMIN] == want.Cc[unix.VMIN] && cur.Cc[unix.VTIME] == want.Cc[unix.VTIME] &&
		cur.Lflag&unix.ICANON == want.Lflag&unix.ICANON && cur.Lflag&unix.ECHO == want.Lflag&unix.ECHO {
		return nil
	}
	// 不 flush：用户可能正输入一个多字节序列，冲输入队列会吞键。
	return ctty.SetTermios(fd, want)
}

func readerTermios(saved ctty.Termios) ctty.Termios {
	raw := keysTermios(saved)
	raw.Oflag = saved.Oflag
	return raw
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

func (t *posixTTY) Sane() {
	fd := int(t.in.Fd())
	cur, err := ctty.GetTermios(fd)
	if err != nil {
		return
	}
	if sane := saneTermios(cur); sane != cur {
		_ = ctty.SetTermios(fd, sane)
	}
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
	fd := int(t.in.Fd())
	ready, err := pollReadable(fd)
	if err != nil {
		return 0, err
	}
	if !ready {
		return 0, nil
	}
	n, err := unix.Read(fd, p)
	if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
		return 0, nil
	}
	return n, err
}

const readPollMillis = 100

func pollReadable(fd int) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, readPollMillis)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		if n == 0 {
			return false, nil
		}
		if fds[0].Revents&unix.POLLIN != 0 {
			return true, nil
		}
		if fds[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return false, unix.EIO
		}
		return false, nil
	}
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

func (t *posixTTY) backgroundRead() bool { return true }

// attachable：raw 模式带 VTIME=1，空闲读每 100ms 返回一次 errIdle，注入事件据此被取走。
func (t *posixTTY) attachable() bool { return true }

func (t *posixTTY) readEvent() (Event, error) {
	ev, err := t.keys.readKey()
	if err != nil {
		return Event{}, err
	}
	return keyEvent(ev), nil
}
