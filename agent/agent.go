package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const DefaultSystemPrompt = `你是 tanyan（兼容 Pi/opencode），运行在终端中的极简编码代理。
通过 run_shell 工具读取文件、执行命令、修改代码，完成用户交给的任务。
习惯先制定方案：动手前列出实施计划并敲定每个实施细节，仅在用户明确同意后才开始实施。
回答简洁直接；调用工具前用一句话说明要做什么；操作文件时明确显示路径。
文件操作（ls、rg、find、cat 等）优先通过 run_shell 执行。
坚持迭代直到任务完成：修改后主动验证（编译、测试、运行），确认无误再收尾。`

const NoShellSystemPrompt = `你是 tanyan（兼容 Pi/opencode），运行在终端中的极简编码代理。
习惯先制定方案：动手前列出实施计划并敲定每个实施细节，仅在用户明确同意后才开始实施。
回答简洁直接；操作文件时明确显示路径。
坚持迭代直到任务完成：修改后主动验证（编译、测试、运行），确认无误再收尾。`

type Agent struct {
	cfg            *Config
	client         *Client
	history        []Message
	cwd            string
	probe          envProbeFunc
	promptSnapshot string
	legacySystem   bool
	sessionDir     string
	sessionPath    string
	saved          int
	systemSaved    bool
	lastUsage      *Usage
	sessionCache   map[string]SessionInfo
	sessionStat    map[string]sessionFileStat
}

type ResponseInfo struct {
	Duration       time.Duration
	FirstEvent     time.Duration
	FirstReasoning time.Duration
	FirstContent   time.Duration
	Usage          *Usage
	ContextTokens  int
}

type sessionFileStat struct {
	mtime time.Time
	size  int64
}

func New(cfg *Config) (*Agent, error) {
	InitShell(cfg.Shell)
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	sessionDir := resolveSessionDir(cfg, cwd)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, err
	}
	a := &Agent{
		cfg:          cfg,
		client:       NewClient(cfg),
		cwd:          cwd,
		probe:        defaultEnvProbe,
		sessionDir:   sessionDir,
		sessionCache: map[string]SessionInfo{},
		sessionStat:  map[string]sessionFileStat{},
	}
	a.NewSession()
	a.refreshSessions()
	return a, nil
}

func resolveSessionDir(cfg *Config, cwd string) string {
	mode := cfg.SessionMode
	if mode == "" {
		mode = "auto"
	}
	localBase := filepath.Join(cwd, ".tanya")
	switch mode {
	case "local":
		return filepath.Join(localBase, "sessions")
	case "global":
		return filepath.Join(cfg.GlobalSession, workspaceID(cwd))
	default:
		if isDir(localBase) {
			return filepath.Join(localBase, "sessions")
		}
		return filepath.Join(cfg.GlobalSession, workspaceID(cwd))
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func workspaceID(dir string) string {
	var b strings.Builder
	for _, r := range dir {
		switch {
		case r == '/' || r == filepath.Separator:
			b.WriteByte('-')
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "root"
	}
	sum := sha256.Sum256([]byte(dir))
	return fmt.Sprintf("%s-%s", name, hex.EncodeToString(sum[:4]))
}

func globalAgentsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tanyan", "AGENTS.md")
}

func readAgentsFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func buildSystemPrompt(cwd string) string {
	prompt := DefaultSystemPrompt
	if ShellRuntime().profile == nil {
		prompt = NoShellSystemPrompt
	}
	if global := readAgentsFile(globalAgentsPath()); global != "" {
		prompt += "\n\n# 全局说明（~/.config/tanyan/AGENTS.md）\n\n" + global
	}
	if project := readAgentsFile(filepath.Join(cwd, "AGENTS.md")); project != "" {
		prompt += "\n\n# 项目说明（AGENTS.md）\n\n" + project
	}
	return prompt
}

func (a *Agent) systemPrompt() string {
	return a.promptSnapshot
}

func (a *Agent) LegacyPrompt() bool {
	return a.legacySystem
}

func isLegacyPrompt(p string) bool {
	return strings.Contains(p, "## 运行环境") || strings.Contains(p, "## 可用工具")
}

func (a *Agent) runtimePrompt() string {
	p := a.promptSnapshot
	if a.probe != nil {
		p += "\n\n" + envSection(a.cwd, a.probe)
	}
	return p
}

func (a *Agent) NewSession() {
	a.history = nil
	a.saved = 0
	a.lastUsage = nil
	a.legacySystem = false
	a.systemSaved = false
	a.promptSnapshot = buildSystemPrompt(a.cwd)
	a.sessionPath = filepath.Join(a.sessionDir, time.Now().Format("20060102-150405")+".jsonl")
}

func (a *Agent) Ask(ctx context.Context, input string, sink EventSink) error {
	mark := len(a.history)
	a.history = append(a.history, Message{Role: "user", Content: input})
	if err := a.runTurn(ctx, sink); err != nil {
		a.history = a.history[:mark]
		return err
	}
	return a.save()
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
			a.lastUsage = resp.Usage
		}
		if len(resp.ToolCalls) == 0 {
			break
		}
		for _, tc := range resp.ToolCalls {
			sink.Emit(Event{Kind: EventToolStart, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments})
			res := a.dispatch(ctx, tc)
			sink.Emit(Event{Kind: EventToolEnd, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments, Result: res})
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

func (a *Agent) dispatch(ctx context.Context, tc ToolCall) ToolResult {
	if tc.Function.Name == "run_shell" {
		var args struct {
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return ToolResult{Text: fmt.Sprintf(MsgParseArgs, err)}
		}
		return ToolResult{Shell: RunShellResult(ctx, args.Command, args.Timeout)}
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
	}
	return t
}

func (a *Agent) ContextInfo() string {
	var tokenLine string
	if a.lastUsage != nil {
		tokenLine = fmt.Sprintf(MsgTokenAPI,
			a.lastUsage.TotalTokens, a.lastUsage.PromptTokens, a.lastUsage.CompletionTokens)
	} else {
		tokenLine = fmt.Sprintf(MsgTokenEstimate, a.totalTokens())
	}
	return fmt.Sprintf(MsgContextInfo, tokenLine, len(a.history), a.sessionPath)
}

func (a *Agent) PromptUsage() string {
	if a.lastUsage != nil {
		return formatTokens(a.lastUsage.PromptTokens)
	}
	return "~" + formatTokens(a.totalTokens())
}

func (a *Agent) PromptCache() string {
	if a.lastUsage == nil {
		return ""
	}
	hit := a.lastUsage.CacheHit()
	if hit <= 0 {
		return ""
	}
	return formatTokens(hit)
}

func (a *Agent) PromptCacheRate() string {
	if a.lastUsage == nil {
		return ""
	}
	hit := a.lastUsage.CacheHit()
	if hit <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2f%%", float64(hit)/float64(a.lastUsage.PromptTokens)*100)
}

func (a *Agent) PromptSummary() string {
	if a.lastUsage == nil || a.lastUsage.CacheHit() <= 0 {
		return a.PromptUsage()
	}
	return formatTokens(a.lastUsage.CacheHit()) + "/" + formatTokens(a.lastUsage.PromptTokens) + " " + a.PromptCacheRate()
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
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

func (a *Agent) ListModels() ([]string, error) { return a.client.ListModels() }

func (a *Agent) save() error {
	if a.saved >= len(a.history) {
		return nil
	}
	f, err := os.OpenFile(a.sessionPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if !a.systemSaved {
		if err := enc.Encode(Message{Role: "system", Content: a.promptSnapshot}); err != nil {
			return err
		}
		a.systemSaved = true
	}
	for _, m := range a.history[a.saved:] {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	a.saved = len(a.history)
	return nil
}

func (a *Agent) LoadSession(id string) error {
	if strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return fmt.Errorf(MsgBadSessionID)
	}
	path := filepath.Join(a.sessionDir, id+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf(MsgSessionGone, id)
	}
	defer f.Close()
	var msgs []Message
	dec := json.NewDecoder(f)
	for {
		var m Message
		if err := dec.Decode(&m); err != nil {
			break
		}
		msgs = append(msgs, m)
	}
	var history []Message
	if len(msgs) > 0 && msgs[0].Role == "system" && msgs[0].Content != "" {
		a.promptSnapshot = msgs[0].Content
		history = msgs[1:]
		a.systemSaved = true
		a.legacySystem = isLegacyPrompt(msgs[0].Content)
	} else {
		a.promptSnapshot = buildSystemPrompt(a.cwd)
		history = msgs
		a.systemSaved = false
		a.legacySystem = false
	}
	a.history = history
	a.sessionPath = path
	a.saved = len(history)
	a.lastUsage = nil
	return nil
}

type SessionInfo struct {
	ID      string
	ModTime time.Time
	Msgs    int
	Summary string
}

func (a *Agent) ListSessions() ([]SessionInfo, error) {
	if err := a.refreshSessions(); err != nil {
		return nil, err
	}
	list := make([]SessionInfo, 0, len(a.sessionCache))
	for _, si := range a.sessionCache {
		list = append(list, si)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID > list[j].ID })
	return list, nil
}

func (a *Agent) refreshSessions() error {
	entries, err := os.ReadDir(a.sessionDir)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		seen[id] = true
		st := sessionFileStat{mtime: info.ModTime(), size: info.Size()}
		if old, ok := a.sessionStat[id]; ok && old == st {
			continue
		}
		a.sessionStat[id] = st
		a.sessionCache[id] = scanSession(filepath.Join(a.sessionDir, e.Name()), id, info.ModTime())
	}
	for id := range a.sessionStat {
		if !seen[id] {
			delete(a.sessionStat, id)
			delete(a.sessionCache, id)
		}
	}
	return nil
}

func scanSession(path, id string, modTime time.Time) SessionInfo {
	si := SessionInfo{ID: id, ModTime: modTime}
	f, err := os.Open(path)
	if err != nil {
		return si
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m Message
		if json.Unmarshal(line, &m) != nil {
			si.Msgs++
			continue
		}
		if m.Role == "system" {
			continue
		}
		si.Msgs++
		if si.Summary == "" && m.Role == "user" && m.Content != "" {
			s := strings.ReplaceAll(m.Content, "\n", " ")
			if utf8.RuneCountInString(s) > 30 {
				s = string([]rune(s)[:30]) + "..."
			}
			si.Summary = s
		}
	}
	return si
}

func ToolDefs() []ToolDef {
	def := func(name, desc, params string) ToolDef {
		var t ToolDef
		t.Type = "function"
		t.Function.Name = name
		t.Function.Description = desc
		t.Function.Parameters = json.RawMessage(params)
		return t
	}
	var defs []ToolDef
	if rt := ShellRuntime(); rt.profile != nil {
		defs = append(defs, def("run_shell", runShellDesc(rt), runShellParams()))
	}
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

func runShellDesc(rt *shellRuntime) string {
	var b strings.Builder
	switch rt.profile.Kind {
	case KindPowerShell:
		fmt.Fprintf(&b, "在 %s pwsh 中执行命令（PowerShell 语法）", runtime.GOOS)
	case KindCmd:
		fmt.Fprintf(&b, "在 %s cmd 中执行命令（cmd 语法）", runtime.GOOS)
	default:
		fmt.Fprintf(&b, "在 %s %s 中执行 shell 命令", runtime.GOOS, rt.profile.Name)
	}
	b.WriteString("，返回 stdout/stderr/退出码。读文件、搜索、文本处理等系统操作都用它。")
	if len(rt.programs) > 0 {
		b.WriteString("可用程序: " + strings.Join(rt.programs, ", "))
	}
	return b.String()
}

func runShellParams() string {
	return fmt.Sprintf(`{"type":"object","properties":{"command":{"type":"string","description":"要执行的命令"},"timeout":{"type":"integer","description":"超时秒数，默认 %d，最大 %d"}},"required":["command"]}`,
		shellTimeoutSec, shellTimeoutLimit)
}

func (a *Agent) ToolOutputLines() int {
	if a.cfg == nil || a.cfg.ToolOutputLines < 1 {
		return 20
	}
	return a.cfg.ToolOutputLines
}
