package agent

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type sessionFileStat struct {
	mtime time.Time
	size  int64
}

type SessionInfo struct {
	ID      string
	ModTime time.Time
	Msgs    int
	Summary string
	Path    string
}

type sessionStore struct {
	dir         string
	disabled    bool
	file        string
	saved       int
	systemSaved bool
	cache       map[string]SessionInfo
	stat        map[string]sessionFileStat
}

func newSessionStore(dir string, disabled bool) *sessionStore {
	return &sessionStore{
		dir:      dir,
		disabled: disabled,
		cache:    map[string]SessionInfo{},
		stat:     map[string]sessionFileStat{},
	}
}

func (s *sessionStore) rotate() {
	s.saved = 0
	s.systemSaved = false
	s.file = filepath.Join(s.dir, time.Now().Format("20060102-150405")+".jsonl")
}

func (s *sessionStore) path() string { return s.file }

func (s *sessionStore) append(msgs []Message, system string) error {
	if s.disabled || s.saved >= len(msgs) {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if !s.systemSaved {
		if err := enc.Encode(Message{Role: "system", Content: system}); err != nil {
			return err
		}
	}
	for _, m := range msgs[s.saved:] {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(s.file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	prev := info.Size()
	n, err := f.Write(buf.Bytes())
	if err == nil && n != buf.Len() {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = f.Truncate(prev)
		return err
	}
	s.systemSaved = true
	s.saved = len(msgs)
	return nil
}

func (s *sessionStore) load(id string) ([]Message, string, error) {
	if strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return nil, "", fmt.Errorf(MsgBadSessionID)
	}
	path := filepath.Join(s.dir, id+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf(MsgSessionGone, id)
	}
	defer f.Close()
	var msgs []Message
	dec := json.NewDecoder(f)
	for {
		var m Message
		if err := dec.Decode(&m); err != nil {
			break
		}
		msgs = append(msgs, m)
	}
	var history []Message
	var system string
	if len(msgs) > 0 && msgs[0].Role == "system" && msgs[0].Content != "" {
		system = msgs[0].Content
		history = msgs[1:]
	} else {
		history = msgs
	}
	s.file = path
	s.saved = len(history)
	s.systemSaved = true
	return history, system, nil
}

func (s *sessionStore) list() ([]SessionInfo, error) {
	if err := s.refresh(); err != nil {
		return nil, err
	}
	list := make([]SessionInfo, 0, len(s.cache))
	for _, si := range s.cache {
		list = append(list, si)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID > list[j].ID })
	return list, nil
}

func (s *sessionStore) refresh() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		seen[id] = true
		st := sessionFileStat{mtime: info.ModTime(), size: info.Size()}
		if old, ok := s.stat[id]; ok && old == st {
			continue
		}
		s.stat[id] = st
		s.cache[id] = scanSession(filepath.Join(s.dir, e.Name()), id, info.ModTime())
	}
	for id := range s.stat {
		if !seen[id] {
			delete(s.stat, id)
			delete(s.cache, id)
		}
	}
	return nil
}

func scanSession(path, id string, modTime time.Time) SessionInfo {
	si := SessionInfo{ID: id, ModTime: modTime, Path: path}
	f, err := os.Open(path)
	if err != nil {
		return si
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m Message
		if json.Unmarshal(line, &m) != nil {
			si.Msgs++
			continue
		}
		if m.Role == "system" {
			continue
		}
		si.Msgs++
		if si.Summary == "" && m.Role == "user" && m.Content != "" {
			s := strings.ReplaceAll(m.Content, "\n", " ")
			if utf8.RuneCountInString(s) > 30 {
				s = string([]rune(s)[:30]) + "..."
			}
			si.Summary = s
		}
	}
	return si
}

func resolveSessionDir(cfg *Config, cwd string) string {
	mode := cfg.SessionMode
	if mode == "" {
		mode = "auto"
	}
	localBase := filepath.Join(cwd, ".tanya")
	switch mode {
	case "local":
		return filepath.Join(localBase, "sessions")
	case "global":
		return filepath.Join(cfg.GlobalSession, workspaceID(cwd))
	default:
		if isDir(localBase) {
			return filepath.Join(localBase, "sessions")
		}
		return filepath.Join(cfg.GlobalSession, workspaceID(cwd))
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func workspaceID(dir string) string {
	var b strings.Builder
	for _, r := range dir {
		switch {
		case r == '/' || r == filepath.Separator:
			b.WriteByte('-')
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "root"
	}
	sum := sha256.Sum256([]byte(dir))
	return fmt.Sprintf("%s-%s", name, hex.EncodeToString(sum[:4]))
}
