package attach_test

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LaoQi/tanya/attach"
)

type fakeHandler struct {
	mu      sync.Mutex
	force   bool
	reply   attach.Result
	block   chan struct{}
	entered chan struct{}
	once    sync.Once
	sawGone bool
}

func (h *fakeHandler) Takeover(force bool, peer attach.Peer, reply func(attach.Result) error) {
	h.once.Do(func() {
		if h.entered != nil {
			close(h.entered)
		}
	})
	h.mu.Lock()
	h.force = force
	block, res := h.block, h.reply
	h.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-peer.Gone():
			h.mu.Lock()
			h.sawGone = true
			h.mu.Unlock()
			return
		}
	}
	_ = reply(res)
}

func (h *fakeHandler) state() (bool, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.force, h.sawGone
}

func serve(t *testing.T, h attach.Handler) (string, attach.Entry) {
	t.Helper()
	dir := t.TempDir()
	srv, err := attach.Serve(dir, os.Getpid(), h)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	e := attach.Entry{PID: os.Getpid(), Sock: srv.SockPath(), State: attach.StateIdle, Title: "标题", TTY: "pts/3", Started: time.Now(), Updated: time.Now()}
	if err := attach.WriteEntry(dir, e); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	got, err := attach.List(dir)
	if err != nil || len(got) != 1 {
		t.Fatalf("List: %v %d", err, len(got))
	}
	return dir, got[0]
}

func TestHandshakeAndTakeover(t *testing.T) {
	h := &fakeHandler{reply: attach.Result{OK: true, Session: "20261012-190122", File: "/tmp/x.jsonl", Messages: 7}}
	dir, e := serve(t, h)
	if e.Title != "标题" || e.TTY != "pts/3" || e.State != attach.StateIdle {
		t.Fatalf("entry 字段丢失: %+v", e)
	}
	if e.Path != filepath.Join(dir, filepath.Base(e.Path)) {
		t.Fatalf("Entry.Path 未指向注册项: %q", e.Path)
	}
	if !attach.Alive(e) {
		t.Fatal("Alive 应为真")
	}
	c, err := attach.Open(e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()
	res, err := c.Takeover(true)
	if err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	if !res.OK || res.Session != "20261012-190122" || res.Messages != 7 {
		t.Fatalf("结果不符: %+v", res)
	}
	if force, _ := h.state(); !force {
		t.Fatal("force 未透传")
	}
}

func TestVersionMismatch(t *testing.T) {
	h := &fakeHandler{reply: attach.Result{OK: true}}
	_, e := serve(t, h)
	raw, err := attach.Dial(e.Sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer raw.Close()
	writeLine(t, raw, `{"hello":{"magic":"tanya-attach","v":99}}`)
	line := readLine(t, raw)
	if !contains(line, `"v":1`) {
		t.Fatalf("服务端应回自己的版本: %q", line)
	}
	if !closedWithin(t, raw, time.Second) {
		t.Fatal("版本不符应关闭连接")
	}
}

func TestBadMagicCloses(t *testing.T) {
	h := &fakeHandler{reply: attach.Result{OK: true}}
	_, e := serve(t, h)
	raw, err := attach.Dial(e.Sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer raw.Close()
	writeLine(t, raw, `{"hello":{"magic":"nope","v":1}}`)
	if !closedWithin(t, raw, time.Second) {
		t.Fatal("魔数不符应关闭连接")
	}
}

func TestPeerGoneCancels(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	h := &fakeHandler{reply: attach.Result{OK: true}, block: block, entered: make(chan struct{})}
	_, e := serve(t, h)
	c, err := attach.Open(e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.Takeover(false)
	}()
	select {
	case <-h.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("服务端未受理请求")
	}
	c.Close()
	waitFor(t, time.Second, func() bool { _, gone := h.state(); return gone })
	<-done
}

func TestStaleEntryNotAlive(t *testing.T) {
	dir := t.TempDir()
	e := attach.Entry{PID: 424242, Sock: filepath.Join(dir, "424242.sock"), State: attach.StateIdle}
	if err := attach.WriteEntry(dir, e); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	got, err := attach.List(dir)
	if err != nil || len(got) != 1 {
		t.Fatalf("List: %v %d", err, len(got))
	}
	if attach.Alive(got[0]) {
		t.Fatal("无 listener 的项应判为陈旧")
	}
	if err := attach.RemoveEntry(dir, 424242); err != nil {
		t.Fatalf("RemoveEntry: %v", err)
	}
	if got, _ = attach.List(dir); len(got) != 0 {
		t.Fatalf("删除后应为空: %d", len(got))
	}
}

func TestListenRemovesStaleSocket(t *testing.T) {
	dir := t.TempDir()
	path := attach.SockPath(dir, os.Getpid())
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatalf("预置残留: %v", err)
	}
	srv, err := attach.Serve(dir, os.Getpid(), &fakeHandler{reply: attach.Result{OK: true}})
	if err != nil {
		t.Fatalf("Serve 应覆盖残留: %v", err)
	}
	defer srv.Close()
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket 权限应为 0600: %v %v", err, fi)
	}
}

func TestCloseRemovesEntry(t *testing.T) {
	dir, _ := serve(t, &fakeHandler{reply: attach.Result{OK: true}})
	got, _ := attach.List(dir)
	if len(got) != 1 {
		t.Fatalf("List: %d", len(got))
	}
	srv, err := attach.Serve(dir, got[0].PID, &fakeHandler{reply: attach.Result{OK: true}})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, _ := attach.List(dir); len(got) != 0 {
		t.Fatalf("Close 后注册项应删除: %d", len(got))
	}
}

func TestNonTakeoverFrameCloses(t *testing.T) {
	_, e := serve(t, &fakeHandler{reply: attach.Result{OK: true}})
	raw, err := attach.Dial(e.Sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer raw.Close()
	writeLine(t, raw, `{"hello":{"magic":"tanya-attach","v":1}}`)
	readLine(t, raw)
	writeLine(t, raw, `{"result":{"ok":true}}`)
	if !closedWithin(t, raw, time.Second) {
		t.Fatal("非 takeover 帧应关闭连接")
	}
}

// badHelloServer 起一个回送指定 hello 帧的假服务端，返回 socket 路径。
func badHelloServer(t *testing.T, frame string) string {
	t.Helper()
	sock := attach.SockPath(t.TempDir(), os.Getpid())
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close(); _ = os.Remove(sock) })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 256)
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.Read(buf)
		_ = c.SetWriteDeadline(time.Now().Add(time.Second))
		_, _ = c.Write([]byte(frame + "\n"))
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.Read(buf)
	}()
	return sock
}

func TestOpenRejectsBadPeerHello(t *testing.T) {
	cases := []struct {
		name  string
		frame string
		want  string
	}{
		{"坏魔数", `{"hello":{"magic":"nope","v":1}}`, attach.MsgBadMagic},
		{"坏版本", `{"hello":{"magic":"tanya-attach","v":99}}`, fmt.Sprintf(attach.MsgBadVersion, attach.Version, 99)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sock := badHelloServer(t, c.frame)
			conn, err := attach.Open(attach.Entry{Sock: sock})
			if err == nil {
				conn.Close()
				t.Fatalf("坏回包应报错: %s", c.frame)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("错误应含 %q: %v", c.want, err)
			}
		})
	}
}

func TestListenLongPathFallsBackToTmp(t *testing.T) {
	deep := t.TempDir()
	for len(attach.SockPath(deep, os.Getpid())) <= 100 {
		deep = filepath.Join(deep, "a-very-long-run-directory")
	}
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(filepath.Join(os.TempDir(), fmt.Sprintf("tanya-run-%d", os.Getpid())))
	srv, err := attach.Serve(deep, os.Getpid(), &fakeHandler{reply: attach.Result{OK: true}})
	if err != nil {
		t.Fatalf("长路径应退临时目录: %v", err)
	}
	sock := srv.SockPath()
	if want := filepath.Join(os.TempDir(), fmt.Sprintf("tanya-run-%d", os.Getpid())); filepath.Dir(sock) != want {
		t.Fatalf("应退到临时目录: %q want %q", sock, want)
	}
	if fi, err := os.Stat(sock); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket 权限应为 0600: %v %v", err, fi)
	}
	c, err := attach.Open(attach.Entry{Sock: sock})
	if err != nil {
		t.Fatalf("退路径的 socket 应可用: %v", err)
	}
	res, err := c.Takeover(false)
	c.Close()
	if err != nil || !res.OK {
		t.Fatalf("退路径上接管应成功: %+v %v", res, err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(sock)); !os.IsNotExist(err) {
		t.Fatal("Close 应清理临时目录")
	}
}

func TestClientStateRereadsEntry(t *testing.T) {
	dir, e := serve(t, &fakeHandler{reply: attach.Result{OK: true}})
	c, err := attach.Open(e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()
	e.State = attach.StateBusy
	e.TurnStarted = time.Now()
	if err := attach.WriteEntry(dir, e); err != nil {
		t.Fatal(err)
	}
	got, err := c.State()
	if err != nil || got.State != attach.StateBusy {
		t.Fatalf("State 应重读到 busy: %+v %v", got, err)
	}
	bare, err := attach.Open(attach.Entry{Sock: e.Sock})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer bare.Close()
	if _, err := bare.State(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("无 Path 时应报 ErrNotExist: %v", err)
	}
}

func TestForceTakeoverTimeout(t *testing.T) {
	old := attach.ForceWaitTimeout
	attach.ForceWaitTimeout = 150 * time.Millisecond
	t.Cleanup(func() { attach.ForceWaitTimeout = old })

	block := make(chan struct{})
	h := &fakeHandler{block: block}
	_, e := serve(t, h)
	c, err := attach.Open(e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()
	start := time.Now()
	_, err = c.Takeover(true)
	if err == nil || !contains(err.Error(), "force 接管超时") {
		t.Fatalf("force 限时应报超时: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("不应无限等待: %v", elapsed)
	}
	close(block)
}

func TestNonForceTakeoverNoDeadline(t *testing.T) {
	old := attach.ForceWaitTimeout
	attach.ForceWaitTimeout = 100 * time.Millisecond
	t.Cleanup(func() { attach.ForceWaitTimeout = old })

	block := make(chan struct{})
	h := &fakeHandler{block: block, reply: attach.Result{OK: true}}
	_, e := serve(t, h)
	c, err := attach.Open(e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()
	res := make(chan attach.Result, 1)
	go func() {
		r, err := c.Takeover(false)
		if err != nil {
			t.Errorf("非 force 排队不应超时: %v", err)
		}
		res <- r
	}()
	time.Sleep(300 * time.Millisecond)
	select {
	case <-res:
		t.Fatal("未放行前不应有结果")
	default:
	}
	close(block)
	if got := <-res; !got.OK {
		t.Fatalf("放行后应完成接管: %+v", got)
	}
}
