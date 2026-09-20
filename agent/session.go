package agent

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	ID       string
	ModTime  time.Time
	Msgs     int
	Summary  string
	Path     string
	Archived bool
	Size     int64
	MetaOK   bool
}

type sessionStore struct {
	dir         string
	archiveDir  string
	disabled    bool
	frozen      bool
	frozenID    string
	file        string
	saved       int
	systemSaved bool
	cache       map[string]SessionInfo
	stat        map[string]sessionFileStat
	volStat     map[string]sessionFileStat
	volumes     map[string][]SessionInfo
}

func newSessionStore(dir, archiveDir string, disabled bool) *sessionStore {
	return &sessionStore{
		dir:        dir,
		archiveDir: archiveDir,
		disabled:   disabled,
		cache:      map[string]SessionInfo{},
		stat:       map[string]sessionFileStat{},
		volStat:    map[string]sessionFileStat{},
		volumes:    map[string][]SessionInfo{},
	}
}

func (s *sessionStore) rotate() {
	s.saved = 0
	s.systemSaved = false
	s.frozen = false
	s.frozenID = ""
	base := time.Now().Format("20060102-150405")
	name := base + ".jsonl"
	for i := 2; ; i++ {
		p := filepath.Join(s.dir, name)
		if _, err := os.Stat(p); err != nil {
			s.file = p
			return
		}
		name = fmt.Sprintf("%s-%d.jsonl", base, i)
	}
}

func (s *sessionStore) path() string { return s.file }

func (s *sessionStore) id() string {
	if s.file == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(s.file), ".jsonl")
}

func (s *sessionStore) append(msgs []Message, system string) error {
	if s.disabled || s.frozen || s.saved >= len(msgs) {
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
	if f, err := os.Open(path); err == nil {
		history, system, err := loadFrom(f)
		f.Close()
		if err != nil {
			return nil, "", err
		}
		s.file = path
		s.frozen = false
		s.frozenID = ""
		s.saved = len(history)
		s.systemSaved = true
		return history, system, nil
	}
	if err := s.refresh(); err != nil {
		return nil, "", err
	}
	volume, ok := s.findArchived(id)
	if !ok {
		return nil, "", fmt.Errorf(MsgSessionGone, id)
	}
	zr, err := zip.OpenReader(volume)
	if err != nil {
		return nil, "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.TrimSuffix(filepath.Base(f.Name), ".jsonl") != id {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, "", err
		}
		history, system, err := loadFrom(rc)
		rc.Close()
		if err != nil {
			return nil, "", err
		}
		s.file = ""
		s.frozen = true
		s.frozenID = id
		s.saved = len(history)
		s.systemSaved = true
		return history, system, nil
	}
	return nil, "", fmt.Errorf(MsgSessionGone, id)
}

func loadFrom(r io.Reader) ([]Message, string, error) {
	var msgs []Message
	dec := json.NewDecoder(r)
	for {
		var m Message
		switch err := dec.Decode(&m); {
		case err == io.EOF:
		case err == nil:
			msgs = append(msgs, m)
			continue
		case tolerantDecodeErr(err):
		default:
			return nil, "", err
		}
		break
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		return nil, "", err
	}
	var history []Message
	var system string
	if len(msgs) > 0 && msgs[0].Role == "system" && msgs[0].Content != "" {
		system = msgs[0].Content
		history = msgs[1:]
	} else {
		history = msgs
	}
	return history, system, nil
}

func tolerantDecodeErr(err error) bool {
	var se *json.SyntaxError
	var ute *json.UnmarshalTypeError
	return errors.As(err, &se) || errors.As(err, &ute) || errors.Is(err, io.ErrUnexpectedEOF)
}

func (s *sessionStore) archivedID() (string, bool) { return s.frozenID, s.frozen }

func (s *sessionStore) findArchived(id string) (string, bool) {
	for path, list := range s.volumes {
		for _, si := range list {
			if si.ID == id {
				return path, true
			}
		}
	}
	return "", false
}

func (s *sessionStore) list() ([]SessionInfo, error) {
	if err := s.refresh(); err != nil {
		return nil, err
	}
	var act, arc []SessionInfo
	for _, si := range s.cache {
		if si.Archived {
			arc = append(arc, si)
		} else {
			act = append(act, si)
		}
	}
	byID := func(list []SessionInfo) {
		sort.Slice(list, func(i, j int) bool { return list[i].ID > list[j].ID })
	}
	byID(act)
	byID(arc)
	return append(act, arc...), nil
}

func (s *sessionStore) refresh() error {
	if err := s.refreshActive(); err != nil {
		return err
	}
	if err := s.refreshVolumes(); err != nil {
		return err
	}
	s.cache = s.activeCache()
	for _, list := range s.volumes {
		for _, si := range list {
			if _, ok := s.cache[si.ID]; !ok {
				s.cache[si.ID] = si
			}
		}
	}
	return nil
}

func (s *sessionStore) refreshActive() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			s.stat = map[string]sessionFileStat{}
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
			if prev, ok := s.cache[id]; ok && !prev.Archived {
				continue
			}
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

func (s *sessionStore) activeCache() map[string]SessionInfo {
	out := make(map[string]SessionInfo, len(s.stat))
	for id := range s.stat {
		if si, ok := s.cache[id]; ok && !si.Archived {
			out[id] = si
		}
	}
	return out
}

func (s *sessionStore) refreshVolumes() error {
	if s.archiveDir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.archiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			s.volStat = map[string]sessionFileStat{}
			s.volumes = map[string][]SessionInfo{}
			return nil
		}
		return err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, archiveVolumeSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(s.archiveDir, name)
		seen[path] = true
		st := sessionFileStat{mtime: info.ModTime(), size: info.Size()}
		if old, ok := s.volStat[path]; ok && old == st {
			if _, ok := s.volumes[path]; ok {
				continue
			}
		}
		s.volStat[path] = st
		vol, err := readVolume(path)
		if err != nil {
			delete(s.volumes, path)
			continue
		}
		list := make([]SessionInfo, 0, len(vol))
		for _, e := range vol {
			list = append(list, e.Info)
		}
		s.volumes[path] = list
	}
	for path := range s.volumes {
		if !seen[path] {
			delete(s.volumes, path)
		}
	}
	for path := range s.volStat {
		if !seen[path] {
			delete(s.volStat, path)
		}
	}
	return nil
}

const sessionSummaryRunes = 30

func scanSession(path, id string, modTime time.Time) SessionInfo {
	si, _ := scanSessionFile(path, id, modTime, sessionSummaryRunes)
	return si
}

func scanSessionFile(path, id string, modTime time.Time, summaryRunes int) (SessionInfo, error) {
	si := SessionInfo{ID: id, ModTime: modTime, Path: path, MetaOK: true}
	f, err := os.Open(path)
	if err != nil {
		return si, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil {
		si.Size = fi.Size()
	}
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
			si.Summary = summarize(m.Content, summaryRunes)
		}
	}
	if err := sc.Err(); err != nil {
		return si, err
	}
	return si, nil
}

func summarize(s string, limit int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if utf8.RuneCountInString(s) > limit {
		return string([]rune(s)[:limit]) + "..."
	}
	return s
}

func resolveWorkspaceDirs(cfg *Config, cwd string) (sessions, archive string) {
	mode := cfg.SessionMode
	if mode == "" {
		mode = "auto"
	}
	localBase := filepath.Join(cwd, ".tanya")
	globalBase := filepath.Join(cfg.DataDir, "workspaces", workspaceID(cwd))
	base := globalBase
	switch mode {
	case "local":
		base = localBase
	case "global":
	default:
		if isDir(localBase) {
			base = localBase
		}
	}
	return filepath.Join(base, "sessions"), filepath.Join(base, "archive")
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
