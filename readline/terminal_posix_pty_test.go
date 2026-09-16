//go:build linux

package readline

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func newPTYTerminal(t *testing.T) (*os.File, *unixTerminal) {
	t.Helper()
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	term, err := openTerminalFile(slave, slave)
	if err != nil {
		master.Close()
		slave.Close()
		t.Fatalf("newUnixTerminalFile: %v", err)
	}
	if err := term.Raw(); err != nil {
		master.Close()
		slave.Close()
		t.Fatalf("Raw: %v", err)
	}
	t.Cleanup(func() {
		term.Restore()
		slave.Close()
		master.Close()
	})
	return master, term
}

type keyResult struct {
	ev  KeyEvent
	err error
}

func readKeyAsync(term *unixTerminal) <-chan keyResult {
	ch := make(chan keyResult, 1)
	go func() {
		ev, err := term.ReadKey()
		ch <- keyResult{ev, err}
	}()
	return ch
}

func consumeHangupRead(t *testing.T, term *unixTerminal) {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := term.readChunk(buf); err != nil && err != unix.EIO {
		t.Fatalf("挂断后首次读取: %v", err)
	}
}

func waitKey(t *testing.T, term *unixTerminal, d time.Duration) keyResult {
	t.Helper()
	select {
	case r := <-readKeyAsync(term):
		return r
	case <-time.After(d):
		t.Fatal("ReadKey 未在预期时间内返回")
		return keyResult{}
	}
}

func TestReadKeyReturnsEOFOnHangup(t *testing.T) {
	master, term := newPTYTerminal(t)
	ch := readKeyAsync(term)
	time.Sleep(150 * time.Millisecond)
	master.Close()
	select {
	case r := <-ch:
		if !errors.Is(r.err, io.EOF) {
			t.Fatalf("pty 挂断后期望 io.EOF，得到 %v", r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pty 挂断后 ReadKey 未返回：仍在校验超时与 EOF 之间空转")
	}
}

func TestReadKeyReturnsEOFAfterHangupReadConsumed(t *testing.T) {
	master, term := newPTYTerminal(t)
	time.Sleep(100 * time.Millisecond)
	master.Close()
	time.Sleep(50 * time.Millisecond)
	consumeHangupRead(t, term)
	ch := readKeyAsync(term)
	select {
	case r := <-ch:
		if !errors.Is(r.err, io.EOF) {
			t.Fatalf("挂断稳态期望 io.EOF，得到 %v", r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("挂断稳态下 ReadKey 未返回：read 恒返回 0 字节时仍在空转")
	}
}

func TestReadKeyIdleIsNotEOF(t *testing.T) {
	master, term := newPTYTerminal(t)
	ch := readKeyAsync(term)
	select {
	case r := <-ch:
		t.Fatalf("空闲时 ReadKey 不应返回: ev=%+v err=%v", r.ev, r.err)
	case <-time.After(350 * time.Millisecond):
	}
	if _, err := master.Write([]byte("a")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("按键读取失败: %v", r.err)
		}
		if r.ev.Code != KeyRune || r.ev.Rune != 'a' {
			t.Fatalf("期望 'a'，得到 %+v", r.ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("按键后 ReadKey 未返回")
	}
}

func TestReadKeyLoneEscAfterIdleTimeout(t *testing.T) {
	master, term := newPTYTerminal(t)
	if _, err := master.Write([]byte{0x1b}); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := waitKey(t, term, 2*time.Second)
	if r.err != nil {
		t.Fatalf("独立 ESC 不应报错: %v", r.err)
	}
	if r.ev.Code != KeyEsc {
		t.Fatalf("期望 KeyEsc，得到 %+v", r.ev)
	}
}

func TestReadKeyQueueSurvivesHangup(t *testing.T) {
	master, term := newPTYTerminal(t)
	if _, err := master.Write([]byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	r := waitKey(t, term, 2*time.Second)
	if r.err != nil || r.ev.Code != KeyRune || r.ev.Rune != 'h' {
		t.Fatalf("首个按键期望 'h'，得到 %+v err=%v", r.ev, r.err)
	}
	master.Close()
	consumeHangupRead(t, term)
	r = waitKey(t, term, 2*time.Second)
	if r.err != nil || r.ev.Code != KeyRune || r.ev.Rune != 'i' {
		t.Fatalf("挂断后队列残留按键应仍返回 'i'，得到 %+v err=%v", r.ev, r.err)
	}
	select {
	case res := <-readKeyAsync(term):
		if !errors.Is(res.err, io.EOF) {
			t.Fatalf("队列排空后期望 io.EOF，得到 %+v err=%v", res.ev, res.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("队列排空后 ReadKey 未返回 io.EOF")
	}
}
