package agent

import (
	"archive/zip"
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

const (
	archiveVolumePrefix  = "archive-"
	archiveVolumeSuffix  = ".zip"
	archiveTempSuffix    = ".tmp-"
	archiveVolumeTimeFmt = "20060102-150405"
	archiveCommentV      = 1
	zipCommentLimit      = 4096
	archiveSummaryRunes  = 200
	archiveSummaryMin    = 80
	archiveIdleGuard     = 5 * time.Minute

	DefaultArchiveThreshold = 64
	DefaultArchiveKeep      = 16
)

type ArchiveOptions struct {
	OlderThan time.Duration
	Keep      int
	Exclude   string
	DryRun    bool
	Now       time.Time
}

type ArchiveEntry struct {
	ID     string
	Before int64
	After  int64
}

type ArchiveSkip struct {
	ID     string
	Reason string
}

type ArchiveFail struct {
	ID  string
	Err error
}

type ArchiveReport struct {
	Volume      string
	DryRun      bool
	Active      int
	Sessions    []ArchiveEntry
	Skipped     []ArchiveSkip
	Failed      []ArchiveFail
	RawBytes    int64
	VolumeBytes int64
}

type archiveEntryComment struct {
	V       int    `json:"v"`
	Msgs    int    `json:"msgs"`
	Summary string `json:"summary,omitempty"`
	Trunc   bool   `json:"trunc,omitempty"`
}

type archiveVolumeComment struct {
	V         int    `json:"v"`
	Workspace string `json:"workspace"`
	Created   string `json:"created"`
	Sessions  int    `json:"sessions"`
}

func (s *sessionStore) archive(opt ArchiveOptions) (ArchiveReport, error) {
	var rep ArchiveReport
	if s.disabled {
		return rep, errors.New(MsgArchiveNoSave)
	}
	now := opt.Now
	if now.IsZero() {
		now = time.Now()
	}
	if err := s.refresh(); err != nil {
		return rep, err
	}
	cands := make([]SessionInfo, 0, len(s.cache))
	for _, si := range s.cache {
		if !si.Archived {
			cands = append(cands, si)
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].ID > cands[j].ID })
	rep.Active = len(cands)
	if opt.Keep > 0 && len(cands) > opt.Keep {
		cands = cands[opt.Keep:]
	}
	picked := make([]SessionInfo, 0, len(cands))
	for _, si := range cands {
		if si.ID == opt.Exclude {
			continue
		}
		if opt.OlderThan > 0 && si.ModTime.After(now.Add(-opt.OlderThan)) {
			continue
		}
		if now.Sub(si.ModTime) < archiveIdleGuard {
			rep.Skipped = append(rep.Skipped, ArchiveSkip{ID: si.ID, Reason: MsgArchiveSkipIdle})
			continue
		}
		if _, ok := s.findArchived(si.ID); ok {
			rep.Skipped = append(rep.Skipped, ArchiveSkip{ID: si.ID, Reason: MsgArchiveSkipArchived})
			continue
		}
		picked = append(picked, si)
	}
	rep.DryRun = opt.DryRun
	for _, si := range picked {
		rep.Sessions = append(rep.Sessions, ArchiveEntry{ID: si.ID, Before: si.Size})
		rep.RawBytes += si.Size
	}
	if opt.DryRun || len(picked) == 0 {
		return rep, nil
	}
	written := make([]SessionInfo, 0, len(picked))
	metas := make([]SessionInfo, 0, len(picked))
	for _, si := range picked {
		meta, err := scanSessionFile(si.Path, si.ID, si.ModTime, archiveSummaryRunes)
		if err != nil {
			rep.Failed = append(rep.Failed, ArchiveFail{ID: si.ID, Err: err})
			continue
		}
		written = append(written, si)
		metas = append(metas, meta)
	}
	if len(written) == 0 {
		return rep, nil
	}
	if err := os.MkdirAll(s.archiveDir, 0o755); err != nil {
		return rep, err
	}
	stamp := now.Format(archiveVolumeTimeFmt)
	volume := filepath.Join(s.archiveDir, archiveVolumePrefix+stamp+archiveVolumeSuffix)
	tmp, err := os.CreateTemp(s.archiveDir, archiveVolumePrefix+stamp+archiveVolumeSuffix+archiveTempSuffix+"*")
	if err != nil {
		return rep, err
	}
	tmpPath := tmp.Name()
	err = writeVolume(tmp, written, metas)
	tmp.Close()
	if err == nil {
		err = os.Rename(tmpPath, volume)
	}
	if err != nil {
		os.Remove(tmpPath)
		return rep, err
	}
	rep.Volume = volume
	if fi, err := os.Stat(volume); err == nil {
		rep.VolumeBytes = fi.Size()
	}
	compressed := map[string]int64{}
	if entries, err := readVolume(volume); err == nil {
		for _, e := range entries {
			compressed[e.Info.ID] = e.Compressed
		}
	}
	rep.Sessions = rep.Sessions[:0]
	for _, si := range written {
		rep.Sessions = append(rep.Sessions, ArchiveEntry{ID: si.ID, Before: si.Size, After: compressed[si.ID]})
		os.Remove(si.Path)
	}
	s.volStat = map[string]sessionFileStat{}
	return rep, nil
}

func writeVolume(tmp *os.File, picked, metas []SessionInfo) error {
	zw := zip.NewWriter(tmp)
	for i, si := range picked {
		src, err := os.Open(si.Path)
		if err != nil {
			return fmt.Errorf(MsgArchiveVolFailFmt, si.ID, err)
		}
		hdr := &zip.FileHeader{
			Name:     si.ID + ".jsonl",
			Method:   zip.Deflate,
			Modified: si.ModTime,
			Comment:  archiveEntryCommentJSON(metas[i].Msgs, metas[i].Summary),
		}
		w, err := zw.CreateHeader(hdr)
		if err == nil {
			_, err = io.Copy(w, src)
		}
		src.Close()
		if err != nil {
			return fmt.Errorf(MsgArchiveVolFailFmt, si.ID, err)
		}
	}
	ws, _ := os.Getwd()
	comment, err := json.Marshal(archiveVolumeComment{V: archiveCommentV, Workspace: ws, Created: time.Now().Format(time.RFC3339), Sessions: len(picked)})
	if err == nil {
		zw.SetComment(string(comment))
	}
	return zw.Close()
}

var archiveCommentLimits = []int{archiveSummaryRunes, archiveSummaryMin, 0}

func archiveEntryCommentJSON(msgs int, summary string) string {
	return archiveEntryCommentLimited(msgs, summary, archiveCommentLimits, zipCommentLimit)
}

func archiveEntryCommentLimited(msgs int, summary string, limits []int, cap int) string {
	for _, limit := range limits {
		c := archiveEntryComment{V: archiveCommentV, Msgs: msgs}
		if summary != "" && limit > 0 {
			c.Summary = summary
			if utf8.RuneCountInString(c.Summary) > limit {
				c.Summary = string([]rune(c.Summary)[:limit])
				c.Trunc = true
			}
		}
		b, err := json.Marshal(c)
		if err != nil {
			continue
		}
		if len(b) <= cap {
			return string(b)
		}
	}
	return fmt.Sprintf(`{"v":%d,"msgs":%d}`, archiveCommentV, msgs)
}

type volumeEntry struct {
	Info       SessionInfo
	Compressed int64
}

func readVolume(path string) ([]volumeEntry, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out := make([]volumeEntry, 0, len(zr.File))
	for _, f := range zr.File {
		id := strings.TrimSuffix(filepath.Base(f.Name), ".jsonl")
		if id == "" {
			continue
		}
		si := SessionInfo{
			ID:       id,
			ModTime:  f.Modified.Local(),
			Path:     path,
			Archived: true,
			Size:     int64(f.UncompressedSize64),
		}
		var c archiveEntryComment
		if f.Comment != "" && json.Unmarshal([]byte(f.Comment), &c) == nil && c.V == archiveCommentV {
			si.Msgs = c.Msgs
			si.Summary = summarize(c.Summary, sessionSummaryRunes)
			si.MetaOK = true
		}
		out = append(out, volumeEntry{Info: si, Compressed: int64(f.CompressedSize64)})
	}
	return out, nil
}
