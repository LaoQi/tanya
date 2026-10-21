package attach

import (
	"os"
	"testing"
	"time"
)

// 探测与删除之间注册项被重写（pid 复用 + 新实例接管同一路径）：重读不一致即跳过。
func TestPruneSkipsEntryRewrittenBetweenProbeAndRemove(t *testing.T) {
	dir := t.TempDir()
	pid := 4701
	if err := WriteEntry(dir, Entry{
		PID: pid, Sock: SockPath(dir, pid), State: StateIdle, Started: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SockPath(dir, pid), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := prune(dir, func() {
		if err := WriteEntry(dir, Entry{
			PID: pid, Sock: SockPath(dir, pid), State: StateIdle,
			Started: time.Now(), Updated: time.Now().Add(time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(rep.Removed) != 0 {
		t.Fatalf("探测后被重写的注册项不得删除: %+v", rep.Removed)
	}
	if len(rep.Kept) != 1 {
		t.Fatalf("应作为保留项报告: %+v", rep.Kept)
	}
	if _, err := os.Stat(EntryPath(dir, pid)); err != nil {
		t.Fatalf("注册项应原样保留: %v", err)
	}
}

func TestPruneCleansTmpSockDir(t *testing.T) {
	dir := t.TempDir()
	pid := 4702
	alt := tmpSockDir(pid)
	if err := os.MkdirAll(alt, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SockPath(alt, pid), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteEntry(dir, Entry{
		PID: pid, Sock: SockPath(alt, pid), State: StateIdle, Started: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := Prune(dir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 1 {
		t.Fatalf("应清掉一条: %+v", rep.Removed)
	}
	if _, err := os.Stat(alt); !os.IsNotExist(err) {
		t.Fatalf("临时 socket 目录应一并删除: %v", err)
	}
}

func TestUnchangedDetectsRewrite(t *testing.T) {
	dir := t.TempDir()
	pid := 4703
	e := Entry{PID: pid, Sock: SockPath(dir, pid), State: StateIdle, Started: time.Now()}
	if err := WriteEntry(dir, e); err != nil {
		t.Fatal(err)
	}
	path := EntryPath(dir, pid)
	got, err := ReadEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged(path, got) {
		t.Fatal("原样重读应判定未变")
	}
	got.Updated = got.Updated.Add(time.Second)
	if unchanged(path, got) {
		t.Fatal("Updated 不同应判定已变")
	}
}
