package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/LaoQi/tanya/agent"
)

type Config struct {
	Override  string
	LookPath  func(string) (string, error)
	Home      string
	Workspace func() string
	Bridge    Bridge
	Programs  []string
}

type request struct {
	Command     string
	TimeoutSec  int
	Interactive bool
	Cwd         string
}

type runArgs struct {
	Command     string `json:"command"`
	Timeout     int    `json:"timeout"`
	Cwd         string `json:"cwd"`
	Interactive bool   `json:"interactive"`
}

func parseRunArgs(raw string) (runArgs, error) {
	var args runArgs
	err := json.Unmarshal([]byte(raw), &args)
	return args, err
}

type Tool struct {
	profile   *profile
	programs  []string
	workspace func() string
	home      string
	bridge    Bridge
	ttyMu     sync.Mutex
}

func New(cfg Config) (*Tool, error) {
	lookPath := cfg.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	p, err := resolveProfile(cfg.Override, lookPath)
	if err != nil {
		return nil, err
	}
	programs := cfg.Programs
	if programs == nil {
		programs = probePrograms(platform.Programs, lookPath)
	}
	return &Tool{
		profile:   p,
		programs:  programs,
		workspace: cfg.Workspace,
		home:      cfg.Home,
		bridge:    cfg.Bridge,
	}, nil
}

func (t *Tool) Name() string { return "run_shell" }

func (t *Tool) Definition() agent.ToolDef {
	return agent.NewToolDef(t.Name(), t.toolDesc(), runShellParams())
}

func (t *Tool) Interactive(argsJSON string) bool {
	args, err := parseRunArgs(argsJSON)
	return err == nil && args.Interactive
}

func (t *Tool) Invoke(ctx context.Context, argsJSON string) agent.ToolResult {
	args, err := parseRunArgs(argsJSON)
	if err != nil {
		return agent.ToolResult{Text: fmt.Sprintf(agent.MsgParseArgs, err)}
	}
	res := t.run(ctx, request{
		Command:     args.Command,
		TimeoutSec:  args.Timeout,
		Interactive: args.Interactive,
		Cwd:         args.Cwd,
	})
	return agent.ToolResult{Text: res.String(), Meta: res}
}

func (t *Tool) run(ctx context.Context, req request) *Result {
	timeoutSec := effectiveTimeout(req.TimeoutSec, req.Interactive)
	explicit := req.Cwd != ""
	dir, err := t.resolveCwd(req.Cwd)
	if err != nil {
		return &Result{Command: req.Command, Err: err.Error()}
	}
	if !explicit {
		dir = t.workspaceDir()
	}
	t.ttyMu.Lock()
	defer t.ttyMu.Unlock()
	if req.Interactive {
		if res, ok := runBridged(ctx, t.bridge, req.Command, timeoutSec, t.profile, dir); ok {
			return defaultCwd(res, explicit)
		}
	}
	return defaultCwd(runForeground(ctx, req.Command, timeoutSec, t.profile, dir, req.Interactive), explicit)
}

func defaultCwd(res *Result, explicit bool) *Result {
	if !explicit {
		res.Cwd = ""
	}
	return res
}

func (t *Tool) workspaceDir() string {
	if t.workspace == nil {
		return ""
	}
	return t.workspace()
}

func (t *Tool) resolveCwd(cwd string) (string, error) {
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
		ws := t.workspaceDir()
		if ws == "" {
			return "", fmt.Errorf(MsgBadCwd, cwd)
		}
		dir = filepath.Join(ws, dir)
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf(MsgBadCwd, cwd)
	}
	return dir, nil
}

func (t *Tool) toolDesc() string {
	return describeShell(platform, t.profile, t.programs)
}

func describeShell(plat shellPlatform, p *profile, programs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "在 %s %s 中执行命令（%s），返回 stdout/stderr/退出码。", plat.GOOS, p.Name, shellSyntaxHint(p.Kind))
	b.WriteString("默认在当前工作区下执行，无需 cd 进入项目；需要其它目录时用 cwd 参数，不必写 cd 前缀。")
	b.WriteString(plat.Capabilities(p))
	b.WriteString("读文件、搜索、文本处理等系统操作都用它。")
	if len(programs) > 0 {
		b.WriteString("可用程序: " + strings.Join(programs, ", "))
	}
	return b.String()
}

func shellSyntaxHint(kind Kind) string {
	switch kind {
	case KindPowerShell:
		return "PowerShell 语法"
	case KindCmd:
		return "cmd 语法"
	default:
		return "shell 语法"
	}
}

func runShellParams() string {
	return fmt.Sprintf(`{"type":"object","properties":{"command":{"type":"string","description":"要执行的命令"},"cwd":{"type":"string","description":"命令执行目录，默认当前工作区"},"timeout":{"type":"integer","description":"超时秒数，默认 %d（interactive 时 %d），最大 %d"},"interactive":{"type":"boolean","description":"命令需要用户在终端应答（sudo/ssh/gpg/read 等交互提示）时置 true：命令与终端直通、可直接应答（Linux 独立 pty、Windows 继承控制台），停用等待动画，默认超时放宽"}},"required":["command"]}`,
		TimeoutSec, InteractiveTimeoutSec, TimeoutLimit)
}
