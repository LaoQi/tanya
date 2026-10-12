package attach_test

import (
	"os"
	"path/filepath"
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
	sawGone bool
}

func (h *fakeHandler) Takeover(force bool, peer attach.Peer, reply func(attach.Result) error) {
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
	h := &fakeHandler{reply: attach.Result{OK: true}, block: block}
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
	time.Sleep(50 * time.Millisecond)
	c.Close()
	waitFor(t, time.Second, func() bool { _, gone := h.state(); return gone })
	<-done
}

func TestStaleEntrySkips(t *testing.T) {
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
