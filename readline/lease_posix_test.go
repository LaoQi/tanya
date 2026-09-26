//go:build linux

package readline

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LaoQi/tanya/ctty"
	"golang.org/x/sys/unix"
)

func newTestPTYPair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("打开 /dev/ptmx: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		t.Skipf("解锁 pty: %v", err)
	}
	ptn, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		t.Skipf("取 pty 号: %v", err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(ptn), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Skipf("打开 pty slave: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	return master, slave
}

func drainPTY(t *testing.T, master *os.File, budget time.Duration) string {
	t.Helper()
	fd := int(master.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatalf("设非阻塞: %v", err)
	}
	var out []byte
	buf := make([]byte, 512)
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		ev, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 50)
		if err != nil && err != syscall.EINTR {
			break
		}
		if ev == 0 {
			continue
		}
		n, err := unix.Read(fd, buf)
		if n > 0 {
			out = append(out, buf[:n]...)
			continue
		}
		if err == unix.EIO {
			break
		}
		if err != nil && err != unix.EAGAIN {
			break
		}
	}
	return string(out)
}

func swapLendOpen(open func() (*os.File, error)) func() {
	prev := lendOpenTTY
	lendOpenTTY = open
	return func() { lendOpenTTY = prev }
}

func TestLendStdinIsNullDevice(t *testing.T) {
	master, slave := newTestPTYPair(t)
	defer swapLendOpen(func() (*os.File, error) { return slave, nil })()

	lease, err := newStdinLease()
	if err != nil {
		t.Fatal(err)
	}
	f := lease.Stdin()
	if f == nil {
		t.Fatal("LendStdin 应交出 stdin")
	}
	if f.Name() != os.DevNull {
		t.Fatalf("stdin 应接 %s，得 %q", os.DevNull, f.Name())
	}
	buf := make([]byte, 8)
	if n, err := f.Read(buf); n != 0 || err == nil {
		t.Errorf("空设备应立即 EOF: n=%d err=%v", n, err)
	}
	lease.Release()
	lease.Release()
	drainPTY(t, master, 200*time.Millisecond)
}

func TestLendStdinAnchorsTerminal(t *testing.T) {
	master, slave := newTestPTYPair(t)
	defer swapLendOpen(func() (*os.File, error) { return slave, nil })()

	lease, err := newStdinLease()
	if err != nil {
		t.Fatal(err)
	}
	lease.Stdin().Close()
	if _, err := slave.WriteString("CHILD-MARK"); err != nil {
		t.Fatal(err)
	}
	lease.Release()

	stream := drainPTY(t, master, 3*time.Second)
	save := strings.Index(stream, "\x1b7")
	marker := strings.Index(stream, "CHILD-MARK")
	resetAt := strings.LastIndex(stream, "\x1b[r")
	restore := strings.LastIndex(stream, "\x1b8")
	if save < 0 || marker < 0 || resetAt < 0 || restore < 0 {
		t.Fatalf("锚点序列不完整: %q", stream)
	}
	altExit := strings.Index(stream, "\x1b[?1049l")
	if altExit < 0 {
		t.Fatalf("复位段缺 ?1049l: %q", stream)
	}
	if !(save < marker && marker < altExit && altExit < resetAt && resetAt < restore) {
		t.Errorf("顺序应为 存档 < 子进程 < 模式复位 < 滚动区复位 < 恢复（得 save=%d marker=%d altExit=%d reset=%d restore=%d）: %q",
			save, marker, altExit, resetAt, restore, stream)
	}
}

func TestLendStdinWithoutTerminal(t *testing.T) {
	defer swapLendOpen(func() (*os.File, error) { return nil, errors.New("无控制终端") })()

	lease, err := newStdinLease()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if got := lease.Stdin(); got == nil || got.Name() != os.DevNull {
		t.Fatalf("无终端时仍应接空设备: %v", got)
	}
	lease.Release()
}

func TestLendStdinRestoresTermios(t *testing.T) {
	_, slave := newTestPTYPair(t)
	defer swapLendOpen(func() (*os.File, error) { return slave, nil })()

	probe, err := os.OpenFile(slave.Name(), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("打开 pty slave 副本: %v", err)
	}
	defer probe.Close()
	fd := int(probe.Fd())
	before, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("GetTermios: %v", err)
	}
	lease, err := newStdinLease()
	if err != nil {
		t.Fatal(err)
	}
	raw := before
	raw.Iflag &^= unix.ICRNL | unix.IXON
	raw.Lflag &^= unix.ISIG | unix.ICANON | unix.ECHO | unix.IEXTEN
	raw.Oflag &^= unix.OPOST | unix.ONLCR
	if err := ctty.SetTermios(fd, raw); err != nil {
		t.Fatalf("SetTermios: %v", err)
	}
	lease.Release()
	got, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("GetTermios: %v", err)
	}
	if got != before {
		t.Fatalf("termios 未复原:\n before=%+v\n after =%+v", before, got)
	}
}
