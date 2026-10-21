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
	workspace   string
	disabled    bool
	frozen      bool
	held        bool
	frozenID    string
	file        string
	saved       int
	systemSaved bool
	bound       bool // 已绑定到一个既有的会话文件（写过 / load 过 / adopt 过）：此后文件必须仍在
	cache       map[string]SessionInfo
	stat        map[string]sessionFileStat
	volStat     map[string]sessionFileStat
	volumes     map[string][]SessionInfo
}

func newSessionStore(dir, archiveDir, workspace string, disabled bool) *sessionStore {
	return &sessionStore{
		dir:        dir,
		archiveDir: archiveDir,
		workspace:  workspace,
		disabled:   disabled,
		cache:      map[string]SessionInfo{},
		stat:       map[string]sessionFileStat{},
		volStat:    map[string]sessionFileStat{},
		volumes:    map[string][]SessionInfo{},
	}
}

func (s *sessionStore) nextID() string {
	base := time.Now().Format("20060102-150405")
	name := base + ".jsonl"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(s.dir, name)); err != nil {
			return strings.TrimSuffix(name, ".jsonl")
		}
		name = fmt.Sprintf("%s-%d.jsonl", base, i)
	}
}

func (s *sessionStore) rotate() { s.rotateTo(s.nextID()) }

func (s *sessionStore) rotateTo(id string) {
	s.saved = 0
	s.systemSaved = false
	s.bound = false
	s.frozen = false
	s.frozenID = ""
	s.file = filepath.Join(s.dir, id+".jsonl")
}

func (s *sessionStore) path() string { return s.file }

func (s *sessionStore) id() string {
	if s.file == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(s.file), ".jsonl")
}

func systemFor(system string, saved bool) string {
	if saved {
		return ""
	}
	return system
}

func encodeMessages(w io.Writer, msgs []Message, system string) error {
	enc := json.NewEncoder(w)
	if system != "" {
		if err := enc.Encode(Message{Role: "system", Content: system}); err != nil {
			return err
		}
	}
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	return nil
}

func (s *sessionStore) append(msgs []Message, system string) error {
	if s.disabled || s.frozen || s.held || s.saved >= len(msgs) {
		return nil
	}
	// 已绑定的会话文件必须仍在：消失即报错，不做静默重建（详见 docs/design.md《会话》）。
	// 只有全新会话（rotate 后、Fork 后，尚未绑定任何文件）的首次写入才允许创建。
	create := !s.bound
	if !create {
		if _, err := os.Stat(s.file); err != nil {
			return fmt.Errorf(MsgSessionFileGone, s.file)
		}
	}
	var buf bytes.Buffer
	if err := encodeMessages(&buf, msgs[s.saved:], systemFor(system, s.systemSaved)); err != nil {
		return err
	}
	flags := os.O_APPEND | os.O_WRONLY
	if create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(s.file, flags, 0o644)
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
	s.bound = true
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
		s.bound = true
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

// adopt 以绝对路径接管一个既存会话文件：目录切到该文件所在处，saved/systemSaved 置位，
// 之后的 append 直接续写同一文件（不 rotate、不改写首行）。
func (s *sessionStore) adopt(path string) ([]Message, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	history, system, err := loadFrom(f)
	if err != nil {
		return nil, "", err
	}
	s.dir = filepath.Dir(path)
	s.file = path
	s.frozen = false
	s.frozenID = ""
	s.held = false
	s.saved = len(history)
	s.systemSaved = true
	s.bound = true
	return history, system, nil
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

// Summarize 按会话列表口径把文本压成一行摘要（换行归一空格，30 字上限）。
func Summarize(s string) string { return summarize(s, sessionSummaryRunes) }

const MaxSessionLineBytes = 64 << 20

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
	sc.Buffer(make([]byte, 64*1024), MaxSessionLineBytes)
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
		if si.Summary == "" && m.Role == "user" {
			switch {
			case m.Content != "":
				si.Summary = summarize(m.Content, summaryRunes)
			case len(m.Images) > 0:
				si.Summary = fmt.Sprintf(MsgSessionImageSummary, len(m.Images))
			}
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

func resolveWorkspaceDirs(cfg *Config, cwd string) (sessions, archive, run string) {
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
	return filepath.Join(base, "sessions"), filepath.Join(base, "archive"), filepath.Join(base, "run")
}

// RunDirFor 返回 cfg 与 cwd 对应的运行实例目录（与 sessions/archive 同 base）。
func RunDirFor(cfg *Config, cwd string) string {
	_, _, run := resolveWorkspaceDirs(cfg, cwd)
	return run
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
