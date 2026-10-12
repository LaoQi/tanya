package attach_test

import (
	"net"
	"strings"
	"testing"
	"time"
)

func writeLine(t *testing.T, c net.Conn, s string) {
	t.Helper()
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := c.Write([]byte(s + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readLine(t *testing.T, c net.Conn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 512)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(buf[:n])
}

func closedWithin(t *testing.T, c net.Conn, d time.Duration) bool {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(d))
	buf := make([]byte, 64)
	for {
		if _, err := c.Read(buf); err != nil {
			return true
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func waitFor(t *testing.T, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}
