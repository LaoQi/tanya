package readline

import (
	"os"
	"testing"
	"time"

	"github.com/LaoQi/tanya/ctty"
	"golang.org/x/sys/unix"
)

func tamperToCooked(t *testing.T, fd int) {
	t.Helper()
	cur, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("GetTermios: %v", err)
	}
	cur.Iflag |= unix.ICRNL | unix.IXON
	cur.Lflag |= unix.ISIG | unix.ICANON | unix.ECHO | unix.IEXTEN
	cur.Oflag |= unix.OPOST | unix.ONLCR
	cur.Cc[unix.VMIN] = 1
	cur.Cc[unix.VTIME] = 0
	if err := ctty.SetTermios(fd, cur); err != nil {
		t.Fatalf("SetTermios: %v", err)
	}
}

func newPTYConsole(t *testing.T, slave *os.File) *consoleImpl {
	t.Helper()
	dev, err := openTerminalFile(slave, slave)
	if err != nil {
		t.Fatalf("openTerminalFile: %v", err)
	}
	con := newConsole(dev)
	t.Cleanup(dev.Restore)
	return con
}

func TestReaderSurvivesTermiosTamper(t *testing.T) {
	_, slave := newTestPTYPair(t)
	defer slave.Close()
	fd := int(slave.Fd())
	con := newPTYConsole(t, slave)

	got := make(chan Event, 16)
	cancel := con.SubscribeKeys(func(ev Event) { got <- ev })
	defer cancel()

	waitFor(t, "常驻读者就位", func() bool {
		cur, err := ctty.GetTermios(fd)
		return err == nil && cur.Cc[unix.VMIN] == 0 && cur.Cc[unix.VTIME] == 1
	})

	tamperToCooked(t, fd)
	time.Sleep(300 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		lease, err := con.LendStdin()
		if err == nil && lease != nil {
			lease.Release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("篡改 termios 后 LendStdin 应正常返回: %v", err)
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatal("termios 被改成行缓冲后 LendStdin 等待过久（读者卡在阻塞 read）")
	}
	con.mu.Lock()
	broken := con.broken
	con.mu.Unlock()
	if broken {
		t.Error("应靠读路径的 poll 周期退出，而非 join 兜底超时（broken 被置位）")
	}
}

func TestReaderReassertsTermiosAfterTamper(t *testing.T) {
	_, slave := newTestPTYPair(t)
	defer slave.Close()
	fd := int(slave.Fd())
	dev, err := openTerminalFile(slave, slave)
	if err != nil {
		t.Fatalf("openTerminalFile: %v", err)
	}
	con := newConsole(dev)
	defer dev.Restore()
	cancel := con.SubscribeKeys(func(Event) {})
	defer cancel()
	waitFor(t, "常驻读者就位", func() bool {
		cur, err := ctty.GetTermios(fd)
		return err == nil && cur.Cc[unix.VMIN] == 0 && cur.Cc[unix.VTIME] == 1
	})
	base := dev.saved

	tamperToCooked(t, fd)
	waitFor(t, "读者应在 1s 内把模式位改回自行轮询的形态", func() bool {
		cur, err := ctty.GetTermios(fd)
		return err == nil && cur.Cc[unix.VMIN] == 0 && cur.Cc[unix.VTIME] == 1 && cur.Lflag&unix.ICANON == 0
	})
	if dev.saved != base {
		t.Error("自愈不应改写退出时的复原基线")
	}
}

func TestReaderDoesNotReassertWhileLent(t *testing.T) {
	_, slave := newTestPTYPair(t)
	defer slave.Close()
	fd := int(slave.Fd())
	con := newPTYConsole(t, slave)
	cancel := con.SubscribeKeys(func(Event) {})
	defer cancel()
	waitFor(t, "常驻读者就位", func() bool {
		cur, err := ctty.GetTermios(fd)
		return err == nil && cur.Cc[unix.VMIN] == 0
	})
	lease, err := con.LendStdin()
	if err != nil {
		t.Fatalf("LendStdin: %v", err)
	}
	defer lease.Release()
	tamperToCooked(t, fd)
	time.Sleep(1500 * time.Millisecond)
	cur, _ := ctty.GetTermios(fd)
	if cur.Cc[unix.VMIN] == 0 {
		t.Error("借出期终端归子进程，读者不应重设模式")
	}
}
