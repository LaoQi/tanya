package attach_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaoQi/tanya/attach"
)

func writeEntry(t *testing.T, dir string, e attach.Entry) {
	t.Helper()
	if err := attach.WriteEntry(dir, e); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
}

// liveSock 在 dir 里起一个真实监听的 socket（模拟存活实例）。
func liveSock(t *testing.T, dir string, pid int) string {
	t.Helper()
	ln, sock, err := attach.Listen(dir, pid)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return sock
}

func staleEntryFiles(t *testing.T, dir string, pid int) {
	t.Helper()
	writeEntry(t, dir, attach.Entry{
		PID: pid, Sock: attach.SockPath(dir, pid), State: attach.StateIdle, Started: time.Now(),
	})
	if err := os.WriteFile(attach.SockPath(dir, pid), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestPruneRemovesStaleEntry(t *testing.T) {
	dir := t.TempDir()
	staleEntryFiles(t, dir, 4242)
	rep, err := attach.Prune(dir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 1 || rep.Removed[0].PID != 4242 || rep.Removed[0].Kind != attach.KindEntry {
		t.Fatalf("应删一条陈旧注册项: %+v", rep.Removed)
	}
	if exists(attach.EntryPath(dir, 4242)) || exists(attach.SockPath(dir, 4242)) {
		t.Fatal("陈旧项的 json 与 sock 都应删除")
	}
	if len(rep.Kept) != 0 {
		t.Fatalf("不应有保留项: %+v", rep.Kept)
	}
}

func TestPruneKeepsAlive(t *testing.T) {
	dir := t.TempDir()
	sock := liveSock(t, dir, 4301)
	writeEntry(t, dir, attach.Entry{
		PID: 4301, Sock: sock, State: attach.StateBusy, TurnStarted: time.Now(), Started: time.Now(),
	})
	rep, err := attach.Prune(dir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 0 {
		t.Fatalf("存活实例（busy）不得删除: %+v", rep.Removed)
	}
	if len(rep.Kept) != 1 || rep.Kept[0].PID != 4301 {
		t.Fatalf("应保留一条: %+v", rep.Kept)
	}
	if !exists(attach.EntryPath(dir, 4301)) || !exists(sock) {
		t.Fatal("存活实例文件必须原样保留")
	}
}

// 注册项 Sock 字段为空时，回落探测 dir 下的确定路径。
func TestPruneEmptySockFieldFallsBack(t *testing.T) {
	dir := t.TempDir()
	sock := liveSock(t, dir, 4302)
	writeEntry(t, dir, attach.Entry{PID: 4302, State: attach.StateIdle, Started: time.Now()})
	if sock != attach.SockPath(dir, 4302) {
		t.Fatalf("sock 路径异常: %s", sock)
	}
	rep, err := attach.Prune(dir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 0 || len(rep.Kept) != 1 {
		t.Fatalf("空 Sock 字段不应据空值判死: removed=%+v kept=%+v", rep.Removed, rep.Kept)
	}

	staleDir := t.TempDir()
	if err := os.WriteFile(attach.EntryPath(staleDir, 4303), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err = attach.Prune(staleDir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 1 || rep.Removed[0].Kind != attach.KindCorrupt || rep.Removed[0].PID != 4303 {
		t.Fatalf("空字段 + 无响应应清掉: %+v", rep.Removed)
	}
}

func TestPruneOrphanSock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(attach.SockPath(dir, 4401), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := attach.Prune(dir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 1 || rep.Removed[0].Kind != attach.KindSock {
		t.Fatalf("孤儿 socket 应清掉: %+v", rep.Removed)
	}
	if exists(attach.SockPath(dir, 4401)) {
		t.Fatal("孤儿 socket 文件应删除")
	}

	aliveDir := t.TempDir()
	liveSock(t, aliveDir, 4402)
	rep, err = attach.Prune(aliveDir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 0 || len(rep.Kept) != 1 {
		t.Fatalf("有响应的孤儿 socket 应保留: removed=%+v kept=%+v", rep.Removed, rep.Kept)
	}
}

func TestPruneCorruptEntry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(attach.EntryPath(dir, 4501), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := attach.Prune(dir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 1 || rep.Removed[0].Kind != attach.KindCorrupt {
		t.Fatalf("不可解析的注册项应清掉: %+v", rep.Removed)
	}

	aliveDir := t.TempDir()
	sock := liveSock(t, aliveDir, 4502)
	if sock != attach.SockPath(aliveDir, 4502) {
		t.Fatalf("sock 路径异常: %s", sock)
	}
	if err := os.WriteFile(attach.EntryPath(aliveDir, 4502), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err = attach.Prune(aliveDir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 0 || len(rep.Kept) != 1 {
		t.Fatalf("坏 json 但 socket 有响应应保留并报告: removed=%+v kept=%+v", rep.Removed, rep.Kept)
	}
}

func TestPruneTmpResidue(t *testing.T) {
	dir := t.TempDir()
	tmp := attach.EntryPath(dir, 4601) + ".tmp"
	if err := os.WriteFile(tmp, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := attach.Prune(dir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(rep.Removed) != 1 || rep.Removed[0].Kind != attach.KindTmp || rep.Removed[0].PID != 4601 {
		t.Fatalf(".json.tmp 残渣应清掉: %+v", rep.Removed)
	}
	if exists(tmp) {
		t.Fatal("tmp 残渣应删除")
	}
}

func TestPruneNoDirAndIdempotent(t *testing.T) {
	rep, err := attach.Prune(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("目录不存在应返回空报告: %v", err)
	}
	if len(rep.Removed) != 0 || len(rep.Kept) != 0 {
		t.Fatalf("空目录应无项: %+v", rep)
	}

	dir := t.TempDir()
	staleEntryFiles(t, dir, 4801)
	if _, err := attach.Prune(dir); err != nil {
		t.Fatal(err)
	}
	rep, err = attach.Prune(dir)
	if err != nil {
		t.Fatalf("第二次 Prune: %v", err)
	}
	if len(rep.Removed) != 0 || len(rep.Kept) != 0 {
		t.Fatalf("幂等：第二次应无项: %+v", rep)
	}
}

func TestSnapshotReportsAlive(t *testing.T) {
	dir := t.TempDir()
	sock := liveSock(t, dir, 4901)
	writeEntry(t, dir, attach.Entry{PID: 4901, Sock: sock, State: attach.StateIdle, Started: time.Now()})
	staleEntryFiles(t, dir, 4902)
	rows, err := attach.Snapshot(dir)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应有两条: %+v", rows)
	}
	byPID := map[int]bool{}
	for _, r := range rows {
		byPID[r.Entry.PID] = r.Alive
	}
	if !byPID[4901] || byPID[4902] {
		t.Fatalf("存活判定错误: %+v", rows)
	}
}
