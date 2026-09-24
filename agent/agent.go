package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Agent struct {
	cfg        *Config
	client     *Client
	tools      *toolRegistry
	workspace  string
	home       string
	bridge     TTYBridge
	history    []Message
	env        string
	basePrompt string
	prompt     *promptBuilder
	store      *sessionStore
	stats      usageStats
}

type ResponseInfo struct {
	Duration       time.Duration
	FirstEvent     time.Duration
	FirstReasoning time.Duration
	FirstContent   time.Duration
	Usage          *Usage
	ContextTokens  int
}

type Options struct {
	noSave       bool
	bridge       TTYBridge
	systemPrompt string
}

type Option func(*Options)

func NoSave(v bool) Option {
	return func(o *Options) { o.noSave = v }
}

func WithTTYBridge(b TTYBridge) Option {
	return func(o *Options) { o.bridge = b }
}

func WithSystemPrompt(s string) Option {
	return func(o *Options) { o.systemPrompt = s }
}

func New(cfg *Config, opts ...Option) (*Agent, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	a := &Agent{cfg: cfg, workspace: cwd, home: home, bridge: o.bridge, basePrompt: o.systemPrompt}
	if err := a.loadWorkspace(cwd, o.noSave); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Agent) loadWorkspace(dir string, noSave bool) error {
	tool, err := newShellTool(shellToolConfig{
		Override:  a.cfg.Shell,
		LookPath:  exec.LookPath,
		Home:      a.home,
		Workspace: dir,
		Bridge:    a.bridge,
	})
	if err != nil {
		return err
	}
	sessionDir, archiveDir := resolveWorkspaceDirs(a.cfg, dir)
	if !noSave {
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			return err
		}
	}
	a.workspace = dir
	a.env = envSection(dir, tool.profile)
	a.prompt = newPromptBuilder(a.basePrompt, dir, globalAgentsPath(), readAgentsFile)
	a.store = newSessionStore(sessionDir, archiveDir, dir, noSave)
	a.tools = newToolRegistry(allTools(tool, a)...)
	a.client = NewClient(a.cfg, a.tools.defs())
	a.NewSession()
	a.store.refresh()
	return nil
}

func (a *Agent) SwitchWorkspace(dir string) error {
	target, err := a.resolveWorkspace(dir)
	if err != nil {
		return err
	}
	if target == a.workspace {
		return fmt.Errorf(MsgSameWorkspace, shortPath(target))
	}
	return a.loadWorkspace(target, a.store.disabled)
}

func (a *Agent) resolveWorkspace(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errors.New(MsgEmptyWorkspace)
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if a.home == "" {
			return "", fmt.Errorf(MsgBadWorkspace, dir)
		}
		if dir == "~" {
			dir = a.home
		} else {
			dir = filepath.Join(a.home, dir[2:])
		}
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(a.workspace, dir)
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf(MsgBadWorkspace, dir)
	}
	return dir, nil
}

func (a *Agent) Workspace() string { return a.workspace }

func (a *Agent) SessionDir() (string, bool) {
	if a.store == nil || a.store.disabled {
		return "", false
	}
	return a.store.dir, true
}

func (a *Agent) systemPrompt() string {
	return a.prompt.system()
}

func (a *Agent) LegacyPrompt() bool {
	return a.prompt.legacyPrompt()
}

func (a *Agent) runtimePrompt() string {
	return a.prompt.runtime(a.env)
}

func (a *Agent) NewSession() {
	a.history = nil
	a.stats.reset()
	a.prompt.reset()
	a.store.rotate()
}

func (a *Agent) save() error {
	return a.store.append(a.history, a.prompt.system())
}

func (a *Agent) LoadSession(id string) error {
	history, system, err := a.store.load(id)
	if err != nil {
		return err
	}
	a.prompt.adopt(system)
	a.history = history
	a.stats.reset()
	return nil
}

func (a *Agent) ListSessions() ([]SessionInfo, error) { return a.store.list() }

func (a *Agent) AutoArchiveKeep() int { return a.cfg.ArchiveKeep }

func (a *Agent) ArchiveSessions(opt ArchiveOptions) (ArchiveReport, error) {
	return a.store.archive(opt)
}

type ArchiveSuggestion struct {
	Threshold  int
	Keep       int
	Active     int
	Candidates int
	Bytes      int64
}

func (a *Agent) SuggestArchive() (ArchiveSuggestion, bool) {
	sug := ArchiveSuggestion{Threshold: a.cfg.ArchiveThreshold, Keep: a.cfg.ArchiveKeep}
	if !a.cfg.AutoArchive || a.store.disabled {
		return sug, false
	}
	list, err := a.store.list()
	if err != nil {
		return sug, false
	}
	cur := a.SessionID()
	active, kept := 0, 0
	for _, si := range list {
		if si.Archived {
			continue
		}
		active++
		if kept < a.cfg.ArchiveKeep {
			kept++
			continue
		}
		if si.ID == cur {
			continue
		}
		sug.Candidates++
		sug.Bytes += si.Size
	}
	if active < a.cfg.ArchiveThreshold {
		return sug, false
	}
	sug.Active = active
	return sug, true
}

func (a *Agent) ArchiveReadOnly() (string, bool) { return a.store.archivedID() }

func (a *Agent) Fork() (string, error) {
	a.store.rotate()
	a.prompt.reset()
	a.stats.reset()
	if err := a.save(); err != nil {
		return "", err
	}
	return a.store.id(), nil
}

const noticeLimit = 200

type InterruptError struct {
	Kept bool
}

func (e *InterruptError) Error() string { return MsgInterruptedBare }

func (e *InterruptError) Unwrap() error { return context.Canceled }

func briefErr(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	r := []rune(s)
	if len(r) > noticeLimit {
		return string(r[:noticeLimit]) + "…"
	}
	return s
}

func (a *Agent) Ask(ctx context.Context, input string, sink EventSink) error {
	if _, ok := a.ArchiveReadOnly(); ok {
		return ErrArchiveReadOnly
	}
	mark := len(a.history)
	a.history = append(a.history, Message{Role: "user", Content: input})
	err := a.runTurn(ctx, sink)
	if err == nil {
		return a.save()
	}
	kept := len(a.history) > mark+1
	if errors.Is(ctx.Err(), context.Canceled) {
		if kept {
			a.history = append(a.history, Message{Role: "user", Content: MsgInterruptNotice})
			_ = a.save()
		} else {
			a.history = a.history[:mark]
		}
		return &InterruptError{Kept: kept}
	}
	if kept {
		a.history = append(a.history, Message{Role: "user", Content: fmt.Sprintf(MsgErrorNoticeFmt, briefErr(err))})
		_ = a.save()
		return err
	}
	a.history = a.history[:mark]
	return err
}

func (a *Agent) runTurn(ctx context.Context, sink EventSink) error {
	for {
		sink.Emit(Event{Kind: EventRequestStart})
		start := time.Now()
		resp, err := a.client.ChatStream(ctx, a.buildMessages(), sink)
		info := ResponseInfo{Duration: time.Since(start)}
		if err == nil {
			if resp.Stat != nil {
				info.Duration = resp.Stat.Duration
				info.FirstEvent = resp.Stat.FirstEvent
				info.FirstReasoning = resp.Stat.FirstReasoning
				info.FirstContent = resp.Stat.FirstContent
			}
			info.Usage = resp.Usage
			if resp.Usage != nil {
				info.ContextTokens = resp.Usage.PromptTokens
			} else {
				info.ContextTokens = a.totalTokens()
			}
		}
		sink.Emit(Event{Kind: EventResponse, Response: info})
		if err != nil {
			return err
		}
		a.history = append(a.history, *resp)
		if resp.Usage != nil {
			a.stats.record(resp.Usage)
		}
		if len(resp.ToolCalls) == 0 {
			break
		}
		for _, tc := range resp.ToolCalls {
			interactive := false
			if tool, ok := a.tools.lookup(tc.Function.Name); ok {
				interactive = interactiveOf(tool, tc.Function.Arguments)
			}
			sink.Emit(Event{Kind: EventToolStart, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments, Interactive: interactive})
			res := a.dispatch(ctx, tc.Function.Name, tc.Function.Arguments)
			sink.Emit(Event{Kind: EventToolEnd, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments, Result: res, Interactive: interactive})
			a.history = append(a.history, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    res.Content(),
			})
		}
	}
	return nil
}

type ToolResult struct {
	Shell *ShellResult
	Text  string
}

func (r ToolResult) Content() string {
	if r.Shell != nil {
		return r.Shell.String()
	}
	return r.Text
}

func (a *Agent) dispatch(ctx context.Context, name, args string) ToolResult {
	tool, ok := a.tools.lookup(name)
	if !ok {
		return ToolResult{Text: fmt.Sprintf(MsgUnknownTool, name)}
	}
	return tool.Invoke(ctx, args)
}

func (a *Agent) buildMessages() []Message {
	msgs := make([]Message, 0, len(a.history)+1)
	msgs = append(msgs, Message{Role: "system", Content: a.runtimePrompt()})
	msgs = append(msgs, a.history...)
	return msgs
}

func estimateTokens(s string) int {
	n := 0.0
	for _, r := range s {
		if r > 127 {
			n += 1
		} else {
			n += 0.3
		}
	}
	return int(n + 0.5)
}

func (a *Agent) totalTokens() int {
	t := estimateTokens(a.runtimePrompt())
	for _, m := range a.history {
		t += estimateTokens(m.Content)
		for _, tc := range m.ToolCalls {
			t += estimateTokens(tc.Function.Arguments)
		}
		for _, r := range m.ReasoningItems {
			t += estimateTokens(r.Content)
		}
	}
	return t
}

func (a *Agent) Stats() Stats {
	st := a.stats.view()
	st.Workspace = a.workspace
	st.Session = a.store.path()
	if id, ok := a.ArchiveReadOnly(); ok {
		st.Archived = id
	}
	st.Messages = len(a.history)
	st.Est = a.totalTokens()
	return st
}

func (a *Agent) Model() string { return a.cfg.Model }

func (a *Agent) SetModel(m string) error {
	m = strings.TrimSpace(m)
	if m == "" {
		return errors.New(MsgEmptyModel)
	}
	a.cfg.Model = m
	return nil
}

func (a *Agent) ReasoningEffort() string { return a.cfg.ReasoningEffort }

func (a *Agent) SetReasoningEffort(level string) error {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "off" {
		a.cfg.ReasoningEffort = ""
		return nil
	}
	v := NormalizeEffort(level)
	if v == "" {
		return fmt.Errorf(MsgBadEffort, level)
	}
	a.cfg.ReasoningEffort = v
	return nil
}

func (a *Agent) History() []Message { return a.history }

func (a *Agent) NoSave() bool { return a.store.disabled }

func (a *Agent) SessionID() string {
	if a.store.disabled {
		return ""
	}
	return a.store.id()
}

func (a *Agent) SessionFile() string {
	if a.store.disabled || a.store.frozen {
		return ""
	}
	p := a.store.path()
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func (a *Agent) ListModels() ([]string, error) { return a.client.ListModels() }

func (a *Agent) ConfigPath() string {
	if a.cfg.ConfigPath == "" {
		return MsgControlUnset
	}
	return a.cfg.ConfigPath
}
