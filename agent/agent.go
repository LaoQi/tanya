package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const DefaultSystemPrompt = `你是 tanyan（兼容 Pi/opencode），运行在终端中的极简编码代理。
通过 run_shell 工具读取文件、执行命令、修改代码，完成用户交给的任务。
习惯先制定方案：动手前列出实施计划并敲定每个实施细节，仅在用户明确同意后才开始实施。
回答简洁直接；调用工具前用一句话说明要做什么；操作文件时明确显示路径。
文件操作（ls、rg、find、cat 等）优先通过 run_shell 执行。
坚持迭代直到任务完成：修改后主动验证（编译、测试、运行），确认无误再收尾。`

type Agent struct {
	cfg     *Config
	client  *Client
	tool    *shellTool
	history []Message
	cwd     string
	probe   envProbeFunc
	prompt  *promptBuilder
	store   *sessionStore
	stats   usageStats
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
		GOOS:      runtime.GOOS,
		LookPath:  exec.LookPath,
		Home:      home,
		Workspace: cwd,
		Bridge:    o.bridge,
	})
	if err != nil {
		return nil, err
	}
	sessionDir := resolveSessionDir(cfg, cwd)
	a := &Agent{
		cfg:    cfg,
		client: NewClient(cfg, ToolDefs(tool)),
		tool:   tool,
		cwd:    cwd,
		probe:  defaultEnvProbe,
		prompt: newPromptBuilder(cwd, globalAgentsPath(), readAgentsFile),
		store:  newSessionStore(sessionDir, o.noSave),
	}
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
	if a.probe == nil {
		return a.prompt.runtime("")
	}
	return a.prompt.runtime(envSection(a.cwd, a.probe, a.tool.profile))
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
			var args runShellArgs
			var argErr error
			if tc.Function.Name == "run_shell" {
				args, argErr = parseRunShellArgs(tc.Function.Arguments)
			}
			sink.Emit(Event{Kind: EventToolStart, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments, Interactive: args.Interactive})
			res := a.dispatch(ctx, tc, args, argErr)
			sink.Emit(Event{Kind: EventToolEnd, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments, Result: res, Interactive: args.Interactive})
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

func (a *Agent) dispatch(ctx context.Context, tc ToolCall, args runShellArgs, argErr error) ToolResult {
	if tc.Function.Name == "run_shell" {
		if argErr != nil {
			return ToolResult{Text: fmt.Sprintf(MsgParseArgs, argErr)}
		}
		return ToolResult{Shell: a.tool.run(ctx, shellRequest{
			Command:     args.Command,
			TimeoutSec:  args.Timeout,
			Interactive: args.Interactive,
			Cwd:         args.Cwd,
		})}
	}
	if text, ok := DispatchBuiltin(tc.Function.Name, tc.Function.Arguments); ok {
		return ToolResult{Text: text}
	}
	return ToolResult{Text: fmt.Sprintf(MsgUnknownTool, tc.Function.Name)}
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

func (a *Agent) ContextInfo() string {
	return a.stats.contextInfo(a.totalTokens(), len(a.history), a.store.path())
}

func (a *Agent) PromptUsage() string {
	return a.stats.promptUsage(a.totalTokens())
}

func (a *Agent) PromptCache() string {
	return a.stats.promptCache()
}

func (a *Agent) PromptCacheRate() string {
	return a.stats.promptCacheRate()
}

func (a *Agent) PromptSummary() string {
	return a.stats.summary(a.totalTokens())
}

func (a *Agent) Model() string { return a.cfg.Model }

func (a *Agent) SetModel(m string) { a.cfg.Model = m }

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

func (a *Agent) ListModels() ([]string, error) { return a.client.ListModels() }

func ToolDefs(tool *shellTool) []ToolDef {
	def := func(name, desc, params string) ToolDef {
		var t ToolDef
		t.Type = "function"
		t.Function.Name = name
		t.Function.Description = desc
		t.Function.Parameters = json.RawMessage(params)
		return t
	}
	defs := []ToolDef{def("run_shell", tool.toolDesc(), runShellParams())}
	return append(defs,
		def("get_time",
			"获取当前日期时间（含时区）",
			`{"type":"object","properties":{}}`),
		def("get_env",
			"获取指定环境变量的值（疑似敏感的变量会被拒绝）",
			`{"type":"object","properties":{"names":{"type":"array","items":{"type":"string"},"description":"环境变量名列表"}},"required":["names"]}`),
		def("calc",
			"计算四则运算表达式，支持 + - * / % 与括号",
			`{"type":"object","properties":{"expression":{"type":"string","description":"算数表达式，如 (1+2)*3/4"}},"required":["expression"]}`),
	)
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

func (a *Agent) ToolOutputLines() int {
	if a.cfg == nil || a.cfg.ToolOutputLines < 1 {
		return 20
	}
	return a.cfg.ToolOutputLines
}
