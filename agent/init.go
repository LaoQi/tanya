package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type InitAction uint8

const (
	InitCreated InitAction = iota
	InitExists
	InitSkipped
)

type InitEntry struct {
	Path   string
	Note   string
	Action InitAction
	Reason string
}

type InitReport struct {
	Workspace  string
	Entries    []InitEntry
	SessionDir string
}

type InitOptions struct {
	ConfirmIgnore func() bool
}

const ignoreFileContent = "*\n"

func InitWorkspace(cfg *Config, o InitOptions) (*InitReport, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	rep := &InitReport{Workspace: cwd}

	e, err := ensureDir(filepath.Join(cwd, ".tanya", "sessions"), MsgInitNoteSessions)
	if err != nil {
		return nil, err
	}
	rep.Entries = append(rep.Entries, e)

	e, err = initIgnore(filepath.Join(cwd, ".tanya", ".gitignore"), o.ConfirmIgnore)
	if err != nil {
		return nil, err
	}
	rep.Entries = append(rep.Entries, e)

	e, err = ensureFile(filepath.Join(cwd, "AGENTS.md"), MsgInitNoteAgents, agentsSkeleton(filepath.Base(cwd)))
	if err != nil {
		return nil, err
	}
	rep.Entries = append(rep.Entries, e)

	rep.SessionDir = resolveSessionDir(cfg, cwd)
	return rep, nil
}

func initIgnore(path string, confirm func() bool) (InitEntry, error) {
	e := InitEntry{Path: path, Note: MsgInitNoteIgnore}
	if fi, err := os.Stat(path); err == nil {
		if fi.Mode().IsRegular() {
			e.Action = InitExists
			return e, nil
		}
		return e, fmt.Errorf(MsgInitFailFmt, path, errors.New(MsgInitNotFile))
	}
	if confirm == nil {
		e.Action = InitSkipped
		e.Reason = MsgInitSkipNoTTY
		return e, nil
	}
	if !confirm() {
		e.Action = InitSkipped
		e.Reason = MsgInitSkipDeclined
		return e, nil
	}
	return ensureFile(path, e.Note, ignoreFileContent)
}

func ensureDir(path, note string) (InitEntry, error) {
	e := InitEntry{Path: path, Note: note}
	fi, err := os.Stat(path)
	switch {
	case err == nil && fi.IsDir():
		e.Action = InitExists
		return e, nil
	case err == nil:
		return e, fmt.Errorf(MsgInitFailFmt, path, errors.New(MsgInitNotDir))
	case !os.IsNotExist(err):
		return e, fmt.Errorf(MsgInitFailFmt, path, err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return e, fmt.Errorf(MsgInitFailFmt, path, err)
	}
	e.Action = InitCreated
	return e, nil
}

func ensureFile(path, note, content string) (InitEntry, error) {
	e := InitEntry{Path: path, Note: note}
	fi, err := os.Stat(path)
	switch {
	case err == nil && fi.Mode().IsRegular():
		e.Action = InitExists
		return e, nil
	case err == nil:
		return e, fmt.Errorf(MsgInitFailFmt, path, errors.New(MsgInitNotFile))
	case !os.IsNotExist(err):
		return e, fmt.Errorf(MsgInitFailFmt, path, err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return e, fmt.Errorf(MsgInitFailFmt, path, err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return e, fmt.Errorf(MsgInitFailFmt, path, err)
	}
	e.Action = InitCreated
	return e, nil
}

const agentsSkeletonFmt = `# %s

<!-- 由 tanyan init 创建；请补全下面各节，AI 每次会话都会读到本文件 -->

## 项目说明

## 构建与测试
`

func agentsSkeleton(name string) string {
	return fmt.Sprintf(agentsSkeletonFmt, name)
}
