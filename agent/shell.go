package agent

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	shellMaxOutput             = 30000
	shellWaitDelay             = 2 * time.Second
	shellTimeoutSec            = 60
	shellInteractiveTimeoutSec = 300
	shellTimeoutLimit          = 900
)

type ShellKind int

const (
	KindPosix ShellKind = iota
	KindPowerShell
	KindCmd
)

type shellProfile struct {
	Path      string
	Name      string
	Kind      ShellKind
	ExtraArgs []string
}

func (p *shellProfile) arg() string {
	switch p.Kind {
	case KindPowerShell:
		return "-Command"
	case KindCmd:
		return "/c"
	default:
		return "-c"
	}
}

func (p *shellProfile) invocation() string {
	parts := make([]string, 0, len(p.ExtraArgs)+2)
	parts = append(parts, p.Path)
	parts = append(parts, p.ExtraArgs...)
	parts = append(parts, p.arg())
	return strings.Join(parts, " ")
}

var shellPrograms = []string{
	"ls", "cat", "head", "tail", "grep", "rg", "fd", "sed", "awk",
	"find", "sort", "wc", "cut", "tr", "xargs",
	"git", "curl", "wget", "go", "node", "python",
}

type shellRuntime struct {
	profile  *shellProfile
	programs []string
}

var (
	shellRuntimeMu  sync.Mutex
	shellRuntimeCur *shellRuntime
	shellRuntimeSet bool
)

func InitShell(override string) {
	shellRuntimeMu.Lock()
	defer shellRuntimeMu.Unlock()
	if shellRuntimeSet {
		return
	}
	shellRuntimeCur = resolveShellRuntime(override, runtime.GOOS, exec.LookPath)
	shellRuntimeSet = true
}

func ShellRuntime() *shellRuntime {
	shellRuntimeMu.Lock()
	defer shellRuntimeMu.Unlock()
	if !shellRuntimeSet {
		shellRuntimeCur = resolveShellRuntime("", runtime.GOOS, exec.LookPath)
		shellRuntimeSet = true
	}
	return shellRuntimeCur
}

func resolveShellRuntime(override, goos string, lookPath func(string) (string, error)) *shellRuntime {
	rt := &shellRuntime{}
	rt.profile = resolveProfile(override, goos, lookPath)
	if rt.profile != nil {
		rt.programs = probePrograms(lookPath)
	}
	return rt
}

func resolveProfile(override, goos string, lookPath func(string) (string, error)) *shellProfile {
	if override != "" {
		if p, err := lookPath(override); err == nil {
			return newProfile(p)
		}
		return nil
	}
	var candidates []string
	if goos == "windows" {
		candidates = []string{"pwsh"}
	} else {
		candidates = []string{"bash", "sh", "ash"}
	}
	for _, name := range candidates {
		if p, err := lookPath(name); err == nil {
			return newProfile(p)
		}
	}
	return nil
}

func newProfile(path string) *shellProfile {
	name := strings.ToLower(filepath.Base(strings.ReplaceAll(path, `\`, "/")))
	name = strings.TrimSuffix(name, ".exe")
	p := &shellProfile{Path: path, Name: name}
	switch name {
	case "powershell", "pwsh":
		p.Kind = KindPowerShell
		p.ExtraArgs = []string{"-NoProfile", "-NonInteractive"}
	case "cmd":
		p.Kind = KindCmd
		p.ExtraArgs = []string{"/d", "/s"}
	default:
		p.Kind = KindPosix
	}
	return p
}

func probePrograms(lookPath func(string) (string, error)) []string {
	var found []string
	for _, name := range shellPrograms {
		if _, err := lookPath(name); err == nil {
			found = append(found, name)
		}
	}
	return found
}

type ShellResult struct {
	Command     string
	Stdout      []ShellChunk
	Stderr      []ShellChunk
	Err         string
	ExitCode    int
	TimedOut    bool
	Interrupted bool
	Stopped     bool
	Duration    time.Duration
}

type ShellChunk struct {
	Data      string
	Truncated int64
}

func (r *ShellResult) String() string {
	var b strings.Builder
	writeStream(&b, "stdout", r.Stdout)
	writeStream(&b, "stderr", r.Stderr)
	if r.Interrupted {
		b.WriteString(MsgInterrupted + "\n")
	}
	if r.TimedOut {
		fmt.Fprintf(&b, MsgTimedOut+"\n")
	}
	if r.Stopped {
		b.WriteString(MsgStopped + "\n")
	}
	if r.Err != "" {
		fmt.Fprintf(&b, MsgErrLine+"\n", r.Err)
	}
	if r.ExitCode != 0 {
		fmt.Fprintf(&b, "exit code: %d\n", r.ExitCode)
	}
	if b.Len() == 0 {
		return MsgNoOutput
	}
	return b.String()
}

func writeStream(sb *strings.Builder, label string, chunks []ShellChunk) {
	if len(chunks) == 0 {
		return
	}
	fmt.Fprintf(sb, "%s:\n%s\n", label, chunks[0].Data)
	if len(chunks) > 1 {
		fmt.Fprintf(sb, MsgTruncMiddle+"\n%s\n", label, chunks[1].Truncated, chunks[1].Data)
	} else if chunks[0].Truncated > 0 {
		fmt.Fprintf(sb, MsgTruncTail+"\n", label, chunks[0].Truncated)
	}
}

type streamCapture struct {
	chunks   *[]ShellChunk
	head     []byte
	tail     []byte
	written  int64
	middle   int64
	headDone bool
}

func (c *streamCapture) Write(p []byte) (int, error) {
	n := len(p)
	c.written += int64(n)
	for len(p) > 0 {
		if !c.headDone {
			space := shellMaxOutput - len(c.head)
			if space > len(p) {
				space = len(p)
			}
			c.head = append(c.head, p[:space]...)
			p = p[space:]
			if len(c.head) == shellMaxOutput {
				c.headDone = true
			}
			continue
		}
		if len(c.tail) == shellMaxOutput {
			c.middle += shellMaxOutput / 2
			c.tail = c.tail[shellMaxOutput/2:]
		}
		space := shellMaxOutput - len(c.tail)
		if space > len(p) {
			space = len(p)
		}
		c.tail = append(c.tail, p[:space]...)
		p = p[space:]
	}
	return n, nil
}

func (c *streamCapture) finish() {
	if c.written == 0 {
		return
	}
	*c.chunks = append(*c.chunks, ShellChunk{Data: string(c.head)})
	if len(c.tail) > 0 {
		if c.middle == 0 {
			(*c.chunks)[0].Data += string(c.tail)
		} else {
			*c.chunks = append(*c.chunks, ShellChunk{Data: string(c.tail), Truncated: c.middle})
		}
	}
}

func RunShell(ctx context.Context, command string, timeoutSec int) string {
	return RunShellResult(ctx, command, timeoutSec).String()
}

const (
	stopPollInterval = 200 * time.Millisecond
	stopPollHits     = 2
)

func waitShell(cmd *exec.Cmd, stopped *bool) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(stopPollInterval)
	defer tick.Stop()
	hits := 0
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
			if processStopped(cmd.Process.Pid) {
				hits++
				if hits >= stopPollHits {
					*stopped = true
					killProcessGroup(cmd)
					return <-done
				}
			} else {
				hits = 0
			}
		}
	}
}

func statState(stat string) string {
	i := strings.IndexByte(stat, ')')
	if i < 0 || i+2 > len(stat) {
		return ""
	}
	fields := strings.Fields(stat[i+2:])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func RunShellResult(ctx context.Context, command string, timeoutSec int) *ShellResult {
	timeoutSec = effectiveShellTimeout(timeoutSec, false)
	res := &ShellResult{Command: command}
	profile := ShellRuntime().profile
	if profile == nil {
		res.Err = MsgShellUnavailable
		return res
	}
	tty := openForegroundTTY()
	handed := false
	defer func() { restoreForeground(tty, handed) }()
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	args := make([]string, 0, len(profile.ExtraArgs)+2)
	args = append(args, profile.ExtraArgs...)
	args = append(args, profile.arg(), command)
	cmd := exec.CommandContext(runCtx, profile.Path, args...)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = shellWaitDelay
	var stdout, stderr streamCapture
	stdout.chunks = &res.Stdout
	stderr.chunks = &res.Stderr
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if tty != nil {
		cmd.Stdin = tty
	}
	if err := cmd.Start(); err != nil {
		res.Err = err.Error()
		res.Duration = time.Since(start)
		return res
	}
	handed = handoverForeground(tty, cmd.Process.Pid)
	err := waitShell(cmd, &res.Stopped)
	stdout.finish()
	stderr.finish()
	res.Duration = time.Since(start)

	switch {
	case ctx.Err() != nil:
		res.Interrupted = true
	case runCtx.Err() == context.DeadlineExceeded:
		res.TimedOut = true
	case res.Stopped:
	case err != nil:
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.Err = err.Error()
		}
	}
	return res
}

func effectiveShellTimeout(explicit int, interactive bool) int {
	if explicit <= 0 {
		if interactive {
			return shellInteractiveTimeoutSec
		}
		return shellTimeoutSec
	}
	if explicit > shellTimeoutLimit {
		return shellTimeoutLimit
	}
	return explicit
}
