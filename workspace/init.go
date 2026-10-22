package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaoQi/battlelab/agent"
)

const (
	MsgInitFailFmt      = "初始化失败 %s: %w"
	MsgInitNotDir       = "已存在但不是目录"
	MsgInitNotFile      = "已存在但不是普通文件"
	MsgInitNoteSessions = "会话与历史"
	MsgInitNoteIgnore   = "忽略 .tanya/ 全部内容"
	MsgInitNoteReadme   = "目录用途说明（勿删）"
	MsgInitNoteAgents   = "项目说明骨架"
	MsgInitSkipNoTTY    = "非交互未询问；如需：echo '*' > .tanya/.gitignore"
	MsgInitSkipDeclined = "已选择不创建；如需：echo '*' > .tanya/.gitignore"
	MsgInitNoLocalDir   = "初始化需要本地会话目录名（Config.LocalDir 为空）"
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

const readmeFileContent = `# .tanya

本目录由 tanya（终端 AI 编码代理）创建，用于存放当前项目的会话数据，请勿删除。

- sessions/  历史会话（每轮对话追加一条记录）
- archive/   归档会话卷（打包为 zip，归档时生成）
- run/       运行实例注册（供 attach 接管运行中的实例，运行时生成）

会话记录删除后无法恢复。会话数据仅供本地使用，建议以 .tanya/.gitignore（内容 *）忽略，避免进入版本库。
`

func InitWorkspace(cfg *agent.Config, o InitOptions) (*InitReport, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	rep := &InitReport{Workspace: cwd}

	if strings.TrimSpace(cfg.LocalDir) == "" {
		return nil, errors.New(MsgInitNoLocalDir)
	}
	local := filepath.Join(cwd, cfg.LocalDir)
	e, err := ensureDir(filepath.Join(local, "sessions"), MsgInitNoteSessions)
	if err != nil {
		return nil, err
	}
	rep.Entries = append(rep.Entries, e)

	e, err = ensureFile(filepath.Join(local, "README"), MsgInitNoteReadme, readmeFileContent)
	if err != nil {
		return nil, err
	}
	rep.Entries = append(rep.Entries, e)

	e, err = initIgnore(filepath.Join(local, ".gitignore"), o.ConfirmIgnore)
	if err != nil {
		return nil, err
	}
	rep.Entries = append(rep.Entries, e)

	e, err = ensureFile(filepath.Join(cwd, "AGENTS.md"), MsgInitNoteAgents, agentsSkeleton(filepath.Base(cwd)))
	if err != nil {
		return nil, err
	}
	rep.Entries = append(rep.Entries, e)

	rep.SessionDir = agent.SessionDirFor(cfg, cwd)
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

<!-- 由 tanya init 创建；请补全下面各节，AI 每次会话都会读到本文件 -->

## 项目说明

## 构建与测试
`

func agentsSkeleton(name string) string {
	return fmt.Sprintf(agentsSkeletonFmt, name)
}
