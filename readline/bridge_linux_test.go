//go:build linux

package readline

import (
	"bytes"
	"github.com/LaoQi/tanyan/ctty"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func readUntil(t *testing.T, fd int, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		if strings.Contains(sb.String(), want) {
			return sb.String()
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			t.Fatalf("等待 %q 超时，已收: %q", want, sb.String())
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(remain.Milliseconds()))
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		if n == 0 {
			t.Fatalf("等待 %q 超时，已收: %q", want, sb.String())
		}
		m, err := unix.Read(fd, buf)
		if err != nil && err != unix.EINTR && err != unix.EAGAIN {
			t.Fatalf("read: %v", err)
		}
		if m > 0 {
			sb.Write(buf[:m])
		}
	}
}

func waitReadable(t *testing.T, fd int, timeout time.Duration) int16 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		fds[0].Revents = 0
		_, err := unix.Poll(fds, 50)
		if err != nil && err != unix.EINTR {
			t.Fatalf("poll: %v", err)
		}
		if fds[0].Revents != 0 {
			return fds[0].Revents
		}
		if time.Now().After(deadline) {
			return 0
		}
	}
}

func TestOpenPTY(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	name := slave.Name()
	if !strings.HasPrefix(name, "/dev/pts/") {
		t.Fatalf("slave 路径异常: %q", name)
	}
	if ws, err := unix.IoctlGetWinsize(int(master.Fd()), unix.TIOCGWINSZ); err != nil || ws == nil {
		t.Fatalf("master 非 pty: %v %v", ws, err)
	}
}

func TestBridgePrepareTwiceRejected(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	b := newBridgeTTY(slave)
	cmd := exec.Command("true")
	if _, err := b.Prepare(cmd); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := b.Prepare(exec.Command("true")); err != ErrUnsupported {
		t.Fatalf("二次 Prepare 应拒绝: %v", err)
	}
	b.release()
}

func TestBridgeInteractiveTTY(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 100}); err != nil {
		t.Fatalf("设置外层尺寸: %v", err)
	}
	before, err := ctty.GetTermios(int(slave.Fd()))
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}

	b := newBridgeTTY(slave)
	cmd := exec.Command("bash", "-c",
		`read x < /dev/tty; echo got:$x; tty; [ -c /dev/tty ] && echo tty-openable; echo slave:$GPG_TTY`)
	var capture bytes.Buffer
	childSlave, err := b.Prepare(cmd)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	childName := childSlave.Name()
	stop, err := b.Attach(&capture)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer stop()

	if raw, err := ctty.GetTermios(int(slave.Fd())); err != nil {
		t.Fatalf("raw getTermios: %v", err)
	} else if raw.Lflag&unix.ICANON != 0 || raw.Lflag&unix.ECHO != 0 || raw.Iflag&unix.ICRNL != 0 {
		t.Errorf("真实 tty 未切 raw: %+v", raw)
	}
	if ws, err := unix.IoctlGetWinsize(b.masterFd, unix.TIOCGWINSZ); err != nil {
		t.Errorf("读回 pty 尺寸: %v", err)
	} else if ws.Row != 24 || ws.Col != 100 {
		t.Errorf("初始尺寸未复制: %+v", ws)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	childSlave.Close()
	if _, err := unix.Write(int(master.Fd()), []byte("hello\n")); err != nil {
		t.Fatalf("写入外层 tty: %v", err)
	}
	out := readUntil(t, int(master.Fd()), "slave:", 5*time.Second)
	if !strings.Contains(out, "got:hello") {
		t.Errorf("子进程未从 /dev/tty 读到输入: %q", out)
	}
	if !strings.Contains(out, childName) {
		t.Errorf("子进程 fd0 非 pty slave: %q want %q", out, childName)
	}
	if !strings.Contains(out, "slave:"+childName) {
		t.Errorf("GPG_TTY 未覆盖为 slave: %q", out)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if re := waitReadable(t, b.masterFd, 2*time.Second); re == 0 {
		t.Error("子进程退出后 master 未收到 EIO（slave 未关闭？）")
	}
	stop()
	if !strings.Contains(capture.String(), "got:hello") {
		t.Errorf("捕获流缺输出: %q", capture.String())
	}
	after, err := ctty.GetTermios(int(slave.Fd()))
	if err != nil {
		t.Fatalf("恢复后 getTermios: %v", err)
	}
	if after != before {
		t.Errorf("termios 未恢复:\n before=%+v\n after =%+v", before, after)
	}
}

func TestBridgeOutputWhileChildAlive(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	b := newBridgeTTY(slave)
	cmd := exec.Command("bash", "-c", `echo ready; sleep 5`)
	var capture bytes.Buffer
	childSlave, err := b.Prepare(cmd)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	stop, err := b.Attach(&capture)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	childSlave.Close()
	readUntil(t, int(master.Fd()), "ready", 5*time.Second)
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("子进程存活时 stop 未及时返回")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if !strings.Contains(capture.String(), "ready") {
		t.Errorf("捕获流缺输出: %q", capture.String())
	}
}

func TestBridgeProductionTTYE2E(t *testing.T) {
	if os.Getenv("TTY_BRIDGE_E2E") == "" {
		t.Skip("需真实 tty: printf 'hello\\n' | script -qec 'TTY_BRIDGE_E2E=1 go test -run TestBridgeProductionTTYE2E -v ./readline' /dev/null")
	}
	b := NewTTYBridge()
	cmd := exec.Command("bash", "-c", `read x < /dev/tty; echo got:$x; tty`)
	var capture bytes.Buffer
	slave, err := b.Prepare(cmd)
	if err != nil {
		t.Fatalf("Prepare（真实 /dev/tty）: %v", err)
	}
	name := slave.Name()
	stop, err := b.Attach(&capture)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer stop()
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	slave.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("命令未在超时内结束（输入未送达？）")
	}
	stop()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !strings.Contains(capture.String(), "got:hello") {
		t.Errorf("捕获流缺输入回显: %q", capture.String())
	}
	if !strings.Contains(capture.String(), name) {
		t.Errorf("子进程 tty 非 pty slave: %q want %q", capture.String(), name)
	}
}

func TestBridgePendingInputPreserved(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	if _, err := unix.Write(int(master.Fd()), []byte("early\n")); err != nil {
		t.Fatalf("预写输入: %v", err)
	}
	b := newBridgeTTY(slave)
	cmd := exec.Command("bash", "-c", `read -r x < /dev/tty; echo got:$x`)
	var capture bytes.Buffer
	childSlave, err := b.Prepare(cmd)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	stop, err := b.Attach(&capture)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer stop()
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	childSlave.Close()
	out := readUntil(t, int(master.Fd()), "got:early", 5*time.Second)
	if !strings.Contains(out, "got:early") {
		t.Fatalf("Attach 前的待读输入被丢弃: %q", out)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	stop()
}

func TestBridgeReusable(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	b := newBridgeTTY(slave)
	for i := 0; i < 2; i++ {
		cmd := exec.Command("bash", "-c", `echo run; tty; echo done`)
		var capture bytes.Buffer
		childSlave, err := b.Prepare(cmd)
		if err != nil {
			t.Fatalf("第 %d 次 Prepare: %v", i+1, err)
		}
		name := childSlave.Name()
		stop, err := b.Attach(&capture)
		if err != nil {
			t.Fatalf("第 %d 次 Attach: %v", i+1, err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatalf("第 %d 次 Start: %v", i+1, err)
		}
		childSlave.Close()
		out := readUntil(t, int(master.Fd()), "done", 5*time.Second)
		if err := cmd.Wait(); err != nil {
			t.Fatalf("第 %d 次 Wait: %v", i+1, err)
		}
		stop()
		if !strings.Contains(out, name) {
			t.Fatalf("第 %d 次 tty 输出异常: %q want %q", i+1, out, name)
		}
		if raw, err := ctty.GetTermios(int(slave.Fd())); err != nil {
			t.Fatal(err)
		} else if raw.Lflag&unix.ICANON == 0 {
			t.Fatalf("第 %d 次 stop 后 termios 未恢复: %+v", i+1, raw)
		}
		if b.master != nil || b.slave != nil {
			t.Fatalf("第 %d 次 stop 后 pty 未释放", i+1)
		}
		if b.tty != slave {
			t.Fatalf("第 %d 次 stop 后注入 tty 被误改: %v", i+1, b.tty)
		}
	}
}

func TestBridgeCtrlCPassthrough(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	b := newBridgeTTY(slave)
	cmd := exec.Command("bash", "-c", `read -r x < /dev/tty; echo never`)
	var capture bytes.Buffer
	childSlave, err := b.Prepare(cmd)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	stop, err := b.Attach(&capture)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer stop()
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	childSlave.Close()
	time.Sleep(100 * time.Millisecond)
	if _, err := unix.Write(int(master.Fd()), []byte{0x03}); err != nil {
		t.Fatalf("写 ^C: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("^C 未送达子进程（仍阻塞在 read）")
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
		t.Fatalf("子进程未被 SIGINT 终止: %v (%v)", err, cmd.ProcessState)
	}
	stop()
	if strings.Contains(capture.String(), "never") {
		t.Errorf("^C 后子进程仍继续执行: %q", capture.String())
	}
}

func statField(stat string) string {
	i := strings.IndexByte(stat, ')')
	if i < 0 || i+2 > len(stat) {
		return ""
	}
	fields := strings.Fields(stat[i+2:])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func TestBridgeKillUnblocksPumps(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	b := newBridgeTTY(slave)
	cmd := exec.Command("bash", "-c", "sleep 30")
	var capture bytes.Buffer
	childSlave, err := b.Prepare(cmd)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	stop, err := b.Attach(&capture)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	childSlave.Close()
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill 进程组: %v", err)
	}
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("进程组被杀后 stop 未及时返回")
	}
	_ = cmd.Wait()
}

func TestBridgeAttachTwiceRejected(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	b := newBridgeTTY(slave)
	cmd := exec.Command("true")
	if _, err := b.Prepare(cmd); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	stop, err := b.Attach(&bytes.Buffer{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if _, err := b.Attach(&bytes.Buffer{}); err != ErrUnsupported {
		t.Fatalf("二次 Attach 应拒绝: %v", err)
	}
	stop()
}

func TestBridgeConcurrentPrepare(t *testing.T) {
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	b := newBridgeTTY(slave)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var okCount int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			f, err := b.Prepare(exec.Command("true"))
			if err == nil {
				atomic.AddInt32(&okCount, 1)
				if f != nil {
					f.Close()
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := atomic.LoadInt32(&okCount); got != 1 {
		t.Fatalf("并发 Prepare 应仅 1 次成功，实际 %d", got)
	}
	b.release()
}
