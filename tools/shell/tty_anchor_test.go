//go:build linux

package shell

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

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

func swapTTYHooks(open func() (*os.File, error), fg func(int) bool) func() {
	prevOpen, prevFg := openTTY, isForeground
	openTTY, isForeground = open, fg
	return func() { openTTY, isForeground = prevOpen, prevFg }
}

func TestRunShellForegroundCursorAnchor(t *testing.T) {
	master, slave := newTestPTYPair(t)
	defer swapTTYHooks(func() (*os.File, error) { return slave, nil }, func(int) bool { return true })()

	res := testShellTool(t).run(context.Background(), request{
		Command:    "printf CHILD-MARK > " + slave.Name(),
		TimeoutSec: 10,
	})
	if res.Err != "" {
		t.Fatalf("命令失败: %+v", res)
	}
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

func TestRunShellForegroundSkipsTerminalWhenNotOwner(t *testing.T) {
	master, slave := newTestPTYPair(t)
	defer swapTTYHooks(func() (*os.File, error) { return slave, nil }, func(int) bool { return false })()

	res := testShellTool(t).run(context.Background(), request{
		Command:    "printf CHILD-MARK > " + slave.Name(),
		TimeoutSec: 10,
	})
	if res.Err != "" {
		t.Fatalf("命令失败: %+v", res)
	}
	stream := drainPTY(t, master, 2*time.Second)
	if strings.Contains(stream, "\x1b") {
		t.Errorf("非前台时不得往终端写任何转义（存档/复位/恢复都要跳过）: %q", stream)
	}
	if !strings.Contains(stream, "CHILD-MARK") {
		t.Errorf("子进程仍应正常执行: %q", stream)
	}
}
