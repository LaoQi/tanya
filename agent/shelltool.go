package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
