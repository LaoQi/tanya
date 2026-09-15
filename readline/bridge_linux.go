//go:build linux

package readline

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/LaoQi/tanyan/ctty"
	"golang.org/x/sys/unix"
)

const bridgeBufSize = 4096

type bridgeTTY struct {
	tty      *os.File
	master   *os.File
	slave    *os.File
	ownTTY   bool
	ttyFd    int
	masterFd int

	saved  ctty.Termios
	rawSet bool

	wakeR int
	wakeW int
	winch chan os.Signal
	done  chan struct{}

	wg       sync.WaitGroup
	stopOnce sync.Once
	relOnce  sync.Once

	mu       sync.Mutex
	busy     bool
	attached bool
}

func NewTTYBridge() TTYBridge { return newBridgeTTY(nil) }

func newBridgeTTY(tty *os.File) *bridgeTTY {
	return &bridgeTTY{tty: tty, wakeR: -1, wakeW: -1}
}

func (b *bridgeTTY) Prepare(cmd *exec.Cmd) (*os.File, error) {
	if !b.acquire() {
		return nil, ErrUnsupported
	}
	b.stopOnce = sync.Once{}
	b.relOnce = sync.Once{}
	if b.tty == nil {
		SecureTerminal()
		tty, err := ctty.Open()
		if err != nil {
			b.release()
			return nil, err
		}
		if !ctty.IsForeground(int(tty.Fd())) {
			tty.Close()
			b.release()
			return nil, ErrUnsupported
		}
		b.tty = tty
		b.ownTTY = true
	}
	master, slave, err := openPTY()
	if err != nil {
		b.release()
		return nil, err
	}
	b.master, b.slave = master, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.Env = ttyEnv(cmd.Env, slave.Name())
	return slave, nil
}

func (b *bridgeTTY) Attach(capture io.Writer) (func(), error) {
	if !b.markAttached() {
		return nil, ErrUnsupported
	}
	b.ttyFd = int(b.tty.Fd())
	b.masterFd = int(b.master.Fd())
	saved, err := ctty.GetTermios(b.ttyFd)
	if err != nil {
		b.release()
		return nil, err
	}
	raw := saved
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Oflag &^= unix.OPOST
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := ctty.SetTermios(b.ttyFd, raw); err != nil {
		b.release()
		return nil, err
	}
	b.saved = saved
	b.rawSet = true
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		b.release()
		return nil, err
	}
	b.wakeR, b.wakeW = pipe[0], pipe[1]
	if err := unix.SetNonblock(b.ttyFd, true); err != nil {
		b.release()
		return nil, err
	}
	if err := unix.SetNonblock(b.masterFd, true); err != nil {
		b.release()
		return nil, err
	}
	if ws, err := unix.IoctlGetWinsize(b.ttyFd, unix.TIOCGWINSZ); err == nil {
		_ = unix.IoctlSetWinsize(b.masterFd, unix.TIOCSWINSZ, ws)
	}
	b.winch = make(chan os.Signal, 1)
	signal.Notify(b.winch, syscall.SIGWINCH)
	b.done = make(chan struct{})
	b.wg.Add(3)
	go b.pumpInput()
	go b.pumpOutput(capture)
	go b.watchWinch()
	return b.stop, nil
}

func (b *bridgeTTY) stop() {
	b.stopOnce.Do(func() {
		if b.winch != nil {
			signal.Stop(b.winch)
		}
		if b.done != nil {
			close(b.done)
		}
		if b.wakeW >= 0 {
			_ = unix.Close(b.wakeW)
			b.wakeW = -1
		}
		b.wg.Wait()
		b.release()
	})
}

func (b *bridgeTTY) acquire() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.busy {
		return false
	}
	b.busy = true
	return true
}

func (b *bridgeTTY) markAttached() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.busy || b.attached || b.master == nil || b.tty == nil {
		return false
	}
	b.attached = true
	return true
}

func (b *bridgeTTY) releaseBusy() {
	b.mu.Lock()
	b.busy = false
	b.attached = false
	b.mu.Unlock()
}

func (b *bridgeTTY) release() {
	b.relOnce.Do(func() {
		defer b.releaseBusy()
		if b.rawSet {
			_ = ctty.SetTermios(b.ttyFd, b.saved)
			if b.tty != nil {
				ctty.ResetModes(b.tty)
			}
			b.rawSet = false
		}
		if b.master != nil {
			_ = b.master.Close()
			b.master = nil
		}
		if b.slave != nil {
			_ = b.slave.Close()
			b.slave = nil
		}
		if b.ownTTY && b.tty != nil {
			_ = b.tty.Close()
			b.tty = nil
			b.ownTTY = false
		}
		if b.wakeR >= 0 {
			_ = unix.Close(b.wakeR)
			b.wakeR = -1
		}
		if b.wakeW >= 0 {
			_ = unix.Close(b.wakeW)
			b.wakeW = -1
		}
	})
}

func (b *bridgeTTY) pumpInput() {
	defer b.wg.Done()
	buf := make([]byte, bridgeBufSize)
	for {
		if !b.waitIO(b.ttyFd, unix.POLLIN) {
			return
		}
		n, err := unix.Read(b.ttyFd, buf)
		if n > 0 && !b.writeAll(b.masterFd, buf[:n]) {
			return
		}
		if err != nil && err != unix.EINTR && err != unix.EAGAIN {
			return
		}
		if n <= 0 && err == nil {
			return
		}
	}
}

func (b *bridgeTTY) pumpOutput(capture io.Writer) {
	defer b.wg.Done()
	defer b.drainOutput(capture)
	buf := make([]byte, bridgeBufSize)
	for {
		if !b.waitIO(b.masterFd, unix.POLLIN) {
			return
		}
		n, err := unix.Read(b.masterFd, buf)
		if n > 0 {
			if capture != nil {
				_, _ = capture.Write(buf[:n])
			}
			if !b.writeAll(b.ttyFd, buf[:n]) {
				return
			}
		}
		if err != nil && err != unix.EINTR && err != unix.EAGAIN {
			return
		}
		if n <= 0 && err == nil {
			return
		}
	}
}

func (b *bridgeTTY) drainOutput(capture io.Writer) {
	buf := make([]byte, bridgeBufSize)
	for {
		n, err := unix.Read(b.masterFd, buf)
		if n > 0 {
			if capture != nil {
				_, _ = capture.Write(buf[:n])
			}
			_ = b.writeAll(b.ttyFd, buf[:n])
		}
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if n <= 0 {
			return
		}
	}
}

func (b *bridgeTTY) watchWinch() {
	defer b.wg.Done()
	for {
		select {
		case <-b.done:
			return
		case <-b.winch:
			if ws, err := unix.IoctlGetWinsize(b.ttyFd, unix.TIOCGWINSZ); err == nil {
				_ = unix.IoctlSetWinsize(b.masterFd, unix.TIOCSWINSZ, ws)
			}
		}
	}
}

func (b *bridgeTTY) waitIO(fd int, events int16) bool {
	fds := []unix.PollFd{
		{Fd: int32(fd), Events: events},
		{Fd: int32(b.wakeR), Events: unix.POLLIN},
	}
	for {
		_, err := unix.Poll(fds, -1)
		if err == unix.EINTR {
			fds[0].Revents, fds[1].Revents = 0, 0
			continue
		}
		if err != nil {
			return false
		}
		if fds[0].Revents&events != 0 {
			return true
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return true
		}
		return false
	}
}

func (b *bridgeTTY) writeAll(fd int, p []byte) bool {
	for len(p) > 0 {
		if !b.waitIO(fd, unix.POLLOUT) {
			return false
		}
		n, err := unix.Write(fd, p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil && err != unix.EINTR && err != unix.EAGAIN {
			return false
		}
		if n <= 0 && err == nil {
			return false
		}
	}
	return true
}

func openPTY() (*os.File, *os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	ptn, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(ptn), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

func ttyEnv(env []string, dev string) []string {
	if env == nil {
		env = os.Environ()
	}
	for _, key := range []string{"GPG_TTY", "SSH_TTY"} {
		env = setEnv(env, key, dev)
	}
	return env
}

func setEnv(env []string, key, val string) []string {
	prefix := key + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}
