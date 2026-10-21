package attach

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	KindEntry   = "entry"
	KindSock    = "sock"
	KindCorrupt = "corrupt"
	KindTmp     = "tmp"
)

// Row 是一条运行实例记录 + 连接探测结果（列表展示用）。
type Row struct {
	Entry Entry
	Alive bool
}

// PrunedItem 是清理报告里的一项。
type PrunedItem struct {
	PID     int
	Kind    string
	Title   string
	Session string
	State   string
	Sock    string
}

// PruneReport 是一次清理的结果：Removed 已删除，Kept 探测有响应故保留。
type PruneReport struct {
	Dir     string
	Removed []PrunedItem
	Kept    []PrunedItem
}

// Snapshot 读出 run 目录里全部注册项并逐个探测存活（不改动任何文件）。
func Snapshot(dir string) ([]Row, error) {
	ents, err := List(dir)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(ents))
	for _, e := range ents {
		rows = append(rows, Row{Entry: e, Alive: Alive(e)})
	}
	return rows, nil
}

// Prune 删除 run 目录里可证明已死的残渣：陈旧注册项（含配对的 socket）、
// 孤儿 socket、不可解析或 pid 与文件名不符的注册项、WriteEntry 中途留下的 .json.tmp。
// 探测有响应的一律保留（含 busy、冻结、归档只读）。
func Prune(dir string) (PruneReport, error) { return prune(dir, nil) }

func prune(dir string, afterProbe func()) (PruneReport, error) {
	rep := PruneReport{Dir: dir}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return rep, nil
		}
		return rep, err
	}
	entries := map[int]bool{}
	socks := map[int]bool{}
	for _, de := range ents {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		switch {
		case strings.HasSuffix(name, ".json.tmp"):
			pid := pidOf(name)
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				continue
			}
			rep.Removed = append(rep.Removed, PrunedItem{PID: pid, Kind: KindTmp})
		case strings.HasSuffix(name, ".json"):
			if pid := pidOf(name); pid > 0 {
				entries[pid] = true
			}
		case strings.HasSuffix(name, ".sock"):
			if pid := pidOf(name); pid > 0 {
				socks[pid] = true
			}
		}
	}
	for pid := range entries {
		path := EntryPath(dir, pid)
		e, err := ReadEntry(path)
		if err != nil || e.PID != pid {
			item := PrunedItem{PID: pid, Kind: KindCorrupt, Sock: SockPath(dir, pid)}
			if aliveSock(dir, pid, "") {
				rep.Kept = append(rep.Kept, item)
				continue
			}
			if removePair(dir, pid, "") {
				rep.Removed = append(rep.Removed, item)
			}
			continue
		}
		item := PrunedItem{PID: pid, Kind: KindEntry, Title: e.Title, Session: e.Session, State: e.State, Sock: e.Sock}
		if afterProbe != nil {
			afterProbe()
		}
		if aliveSock(dir, pid, e.Sock) {
			rep.Kept = append(rep.Kept, item)
			continue
		}
		if !unchanged(path, e) {
			rep.Kept = append(rep.Kept, item)
			continue
		}
		if removePair(dir, pid, e.Sock) {
			rep.Removed = append(rep.Removed, item)
		}
	}
	for pid := range socks {
		if entries[pid] {
			continue
		}
		item := PrunedItem{PID: pid, Kind: KindSock, Sock: SockPath(dir, pid)}
		if aliveSock(dir, pid, "") {
			rep.Kept = append(rep.Kept, item)
			continue
		}
		if removePair(dir, pid, "") {
			rep.Removed = append(rep.Removed, item)
		}
	}
	sortItems(rep.Removed)
	sortItems(rep.Kept)
	return rep, nil
}

func sortItems(items []PrunedItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].PID != items[j].PID {
			return items[i].PID < items[j].PID
		}
		return items[i].Kind < items[j].Kind
	})
}

func pidOf(name string) int {
	base := strings.TrimSuffix(name, ".tmp")
	base = strings.TrimSuffix(base, ".json")
	base = strings.TrimSuffix(base, ".sock")
	n, err := strconv.Atoi(base)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func tmpSockDir(pid int) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("tanya-run-%d", pid))
}

func probePaths(dir string, pid int, sock string) []string {
	if sock != "" {
		return []string{sock}
	}
	primary := SockPath(dir, pid)
	out := []string{primary}
	if alt := SockPath(tmpSockDir(pid), pid); alt != primary {
		out = append(out, alt)
	}
	return out
}

func aliveSock(dir string, pid int, sock string) bool {
	for _, p := range probePaths(dir, pid, sock) {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if Alive(Entry{Sock: p}) {
			return true
		}
	}
	return false
}

func unchanged(path string, want Entry) bool {
	got, err := ReadEntry(path)
	if err != nil {
		return false
	}
	return got.Sock == want.Sock && got.Started.Equal(want.Started) && got.Updated.Equal(want.Updated)
}

func removePair(dir string, pid int, sock string) bool {
	removed := false
	paths := probePaths(dir, pid, sock)
	if sock != "" {
		if p := SockPath(dir, pid); p != sock {
			paths = append(paths, p)
		}
	}
	for _, p := range paths {
		if err := os.Remove(p); err == nil {
			removed = true
		}
		if d := filepath.Dir(p); d != dir && strings.HasPrefix(filepath.Base(d), "tanya-run-") {
			_ = os.Remove(d)
		}
	}
	if err := os.Remove(EntryPath(dir, pid)); err == nil {
		removed = true
	}
	return removed
}
