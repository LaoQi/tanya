package attach

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	StateIdle     = "idle"
	StateBusy     = "busy"
	StateReadOnly = "readonly"
)

// Entry 是 run 目录里的一份运行实例快照：机器用 pid/sock，人看 title/tty/state。
type Entry struct {
	V           int       `json:"v"`
	PID         int       `json:"pid"`
	Sock        string    `json:"sock"`
	TTY         string    `json:"tty,omitempty"`
	Title       string    `json:"title,omitempty"`
	State       string    `json:"state"`
	TurnStarted time.Time `json:"turn_started,omitempty"`
	Session     string    `json:"session,omitempty"`
	Messages    int       `json:"messages,omitempty"`
	Archived    string    `json:"archived,omitempty"`
	Started     time.Time `json:"started"`
	Updated     time.Time `json:"updated"`

	Path string `json:"-"`
}

func EntryPath(dir string, pid int) string { return filepath.Join(dir, fmt.Sprintf("%d.json", pid)) }

func SockPath(dir string, pid int) string { return filepath.Join(dir, fmt.Sprintf("%d.sock", pid)) }

// WriteEntry 原子重写注册项（tmp + rename）：读者不会看到半截 JSON。
func WriteEntry(dir string, e Entry) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	e.V = Version
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := EntryPath(dir, e.PID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ReadEntry(path string) (Entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return Entry{}, err
	}
	e.Path = path
	return e, nil
}

// List 读出 run 目录里全部可解析的注册项（按启动时间排序）。
// 不做删除也不做存活判定：陈旧项由调用方用 Alive 过滤。
func List(dir string) ([]Entry, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Entry, 0, len(ents))
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		e, err := ReadEntry(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out, nil
}

func RemoveEntry(dir string, pid int) error {
	err := os.Remove(EntryPath(dir, pid))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
