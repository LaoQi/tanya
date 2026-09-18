package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const DefaultSystemPrompt = `你是 tanya（兼容 Pi/opencode），运行在终端中的极简编码代理。
通过 run_shell 工具读取文件、执行命令、修改代码，完成用户交给的任务。
习惯先制定方案：动手前列出实施计划并敲定每个实施细节，仅在用户明确同意后才开始实施。
回答简洁直接；调用工具前用一句话说明要做什么；操作文件时明确显示路径。
文件操作（ls、rg、find、cat 等）优先通过 run_shell 执行。
坚持迭代直到任务完成：修改后主动验证（编译、测试、运行），确认无误再收尾。`

type Agent struct {
	cfg       *Config
	client    *Client
	tools     *toolRegistry
	workspace string
	history   []Message
	env       string
	prompt    *promptBuilder
	store     *sessionStore
	stats     usageStats
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
	noSave bool
	bridge TTYBridge
}

type Option func(*Options)

func NoSave(v bool) Option {
	return func(o *Options) { o.noSave = v }
}

func WithTTYBridge(b TTYBridge) Option {
	return func(o *Options) { o.bridge = b }
}

func New(cfg *Config, opts ...Option) (*Agent, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	tool, err := newShellTool(shellToolConfig{
		Override:  cfg.Shell,
		LookPath:  exec.LookPath,
		Home:      home,
		Workspace: cwd,
		Bridge:    o.bridge,
	})
	if err != nil {
		return nil, err
	}
	sessionDir, archiveDir := resolveWorkspaceDirs(cfg, cwd)
	a := &Agent{
		cfg:       cfg,
		workspace: cwd,
		env:       envSection(cwd, tool.profile),
		prompt:    newPromptBuilder(cwd, globalAgentsPath(), readAgentsFile),
		store:     newSessionStore(sessionDir, archiveDir, o.noSave),
	}
	tools := newToolRegistry(allTools(tool, a)...)
	a.tools = tools
	a.client = NewClient(cfg, tools.defs())
	if !o.noSave {
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			return nil, err
		}
	}
	a.NewSession()
	a.store.refresh()
	return a, nil
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
	if _, ok := a.ArchiveReadOnly(); !ok {
		return "", ErrForkNotArchive
	}
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
	v := normalizeEffort(level)
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
	if a.cfg.Path == "" {
		return defaultConfigPath()
	}
	return a.cfg.Path
}

func (a *Agent) ToolOutputLines() int {
	if a.cfg == nil || a.cfg.ToolOutputLines < 1 {
		return 20
	}
	return a.cfg.ToolOutputLines
}
