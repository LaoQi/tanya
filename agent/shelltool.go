package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type shellToolConfig struct {
	Override  string
	GOOS      string
	LookPath  func(string) (string, error)
	Home      string
	Workspace string
	Bridge    TTYBridge
	Programs  []string
}

type shellRequest struct {
	Command     string
	TimeoutSec  int
	Interactive bool
	Cwd         string
}

type runShellArgs struct {
	Command     string `json:"command"`
	Timeout     int    `json:"timeout"`
	Cwd         string `json:"cwd"`
	Interactive bool   `json:"interactive"`
}

func parseRunShellArgs(raw string) (runShellArgs, error) {
	var args runShellArgs
	err := json.Unmarshal([]byte(raw), &args)
	return args, err
}

type shellTool struct {
	profile   *shellProfile
	programs  []string
	workspace string
	home      string
	bridge    TTYBridge
	ttyMu     sync.Mutex
}

func newShellTool(cfg shellToolConfig) (*shellTool, error) {
	profile, err := resolveProfile(cfg.Override, cfg.GOOS, cfg.LookPath)
	if err != nil {
		return nil, err
	}
	programs := cfg.Programs
	if programs == nil {
		programs = probePrograms(cfg.LookPath)
	}
	return &shellTool{
		profile:   profile,
		programs:  programs,
		workspace: cfg.Workspace,
		home:      cfg.Home,
		bridge:    cfg.Bridge,
	}, nil
}

func (t *shellTool) Name() string { return "run_shell" }

func (t *shellTool) Definition() ToolDef {
	return newToolDef(t.Name(), t.toolDesc(), runShellParams())
}

func (t *shellTool) Interactive(argsJSON string) bool {
	args, err := parseRunShellArgs(argsJSON)
	return err == nil && args.Interactive
}

func (t *shellTool) Invoke(ctx context.Context, argsJSON string) ToolResult {
	args, err := parseRunShellArgs(argsJSON)
	if err != nil {
		return ToolResult{Text: fmt.Sprintf(MsgParseArgs, err)}
	}
	return ToolResult{Shell: t.run(ctx, shellRequest{
		Command:     args.Command,
		TimeoutSec:  args.Timeout,
		Interactive: args.Interactive,
		Cwd:         args.Cwd,
	})}
}

func (t *shellTool) run(ctx context.Context, req shellRequest) *ShellResult {
	timeoutSec := effectiveShellTimeout(req.TimeoutSec, req.Interactive)
	dir, err := t.resolveCwd(req.Cwd)
	if err != nil {
		return &ShellResult{Command: req.Command, Err: err.Error()}
	}
	t.ttyMu.Lock()
	defer t.ttyMu.Unlock()
	if req.Interactive {
		if res, ok := runShellBridged(ctx, t.bridge, req.Command, timeoutSec, t.profile, dir); ok {
			return res
		}
	}
	return runShellForeground(ctx, req.Command, timeoutSec, t.profile, dir)
}

func (t *shellTool) resolveCwd(cwd string) (string, error) {
	if cwd == "" {
		return "", nil
	}
	dir := cwd
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if t.home == "" {
			return "", fmt.Errorf(MsgBadCwd, cwd)
		}
		if dir == "~" {
			dir = t.home
		} else {
			dir = filepath.Join(t.home, dir[2:])
		}
	}
	if !filepath.IsAbs(dir) {
		if t.workspace == "" {
			return "", fmt.Errorf(MsgBadCwd, cwd)
		}
		dir = filepath.Join(t.workspace, dir)
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf(MsgBadCwd, cwd)
	}
	return dir, nil
}

func (t *shellTool) toolDesc() string {
	return runShellDesc(t.profile, t.programs)
}

func runShellDesc(profile *shellProfile, programs []string) string {
	var b strings.Builder
	switch profile.Kind {
	case KindPowerShell:
		fmt.Fprintf(&b, "在 %s pwsh 中执行命令（PowerShell 语法）", runtime.GOOS)
	case KindCmd:
		fmt.Fprintf(&b, "在 %s cmd 中执行命令（cmd 语法）", runtime.GOOS)
	default:
		fmt.Fprintf(&b, "在 %s %s 中执行 shell 命令", runtime.GOOS, profile.Name)
	}
	b.WriteString("，返回 stdout/stderr/退出码。读文件、搜索、文本处理等系统操作都用它。")
	b.WriteString("默认在会话启动目录（进程 cwd）下执行，无需 cd 进入项目；需要其它目录时用 cwd 参数，不必写 cd 前缀。")
	if len(programs) > 0 {
		b.WriteString("可用程序: " + strings.Join(programs, ", "))
	}
	return b.String()
}

func runShellParams() string {
	return fmt.Sprintf(`{"type":"object","properties":{"command":{"type":"string","description":"要执行的命令"},"cwd":{"type":"string","description":"命令执行目录，默认会话启动目录"},"timeout":{"type":"integer","description":"超时秒数，默认 %d（interactive 时 %d），最大 %d"},"interactive":{"type":"boolean","description":"命令需要用户在终端应答（sudo/ssh/gpg/read 等交互提示）时置 true：命令在独立 pty 中运行、终端直通应答，停用等待动画，默认超时放宽"}},"required":["command"]}`,
		shellTimeoutSec, shellInteractiveTimeoutSec, shellTimeoutLimit)
}
