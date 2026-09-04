package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const DefaultSystemPrompt = "你是 tanyan，一个运行在命令行中的极简中文 AI 助手。回答简洁直接。需要执行系统操作时优先使用 run_shell 工具。"

type Agent struct {
	cfg         *Config
	client      *Client
	history     []Message
	sessionDir  string
	sessionPath string
	saved       int
	lastUsage   *Usage
	OnTool      func(name, args, result string)
}

func New(cfg *Config) (*Agent, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	sessionDir := resolveSessionDir(cfg, cwd)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, err
	}
	a := &Agent{
		cfg:        cfg,
		client:     NewClient(cfg),
		sessionDir: sessionDir,
	}
	a.NewSession()
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

func (a *Agent) systemPrompt() string {
	if a.cfg.SystemPrompt != "" {
		return a.cfg.SystemPrompt
	}
	return DefaultSystemPrompt
}

func (a *Agent) NewSession() {
	a.history = nil
	a.saved = 0
	a.sessionPath = filepath.Join(a.sessionDir, time.Now().Format("20060102-150405")+".jsonl")
}

func (a *Agent) Ask(ctx context.Context, input string, onDelta func(string)) error {
	a.history = append(a.history, Message{Role: "user", Content: input})
	for {
		resp, err := a.client.ChatStream(ctx, a.buildMessages(), onDelta)
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
			result := a.dispatch(tc)
			if a.OnTool != nil {
				a.OnTool(tc.Function.Name, tc.Function.Arguments, result)
			}
			a.history = append(a.history, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    result,
			})
		}
	}
	return a.save()
}

func (a *Agent) dispatch(tc ToolCall) string {
	if tc.Function.Name == "run_shell" {
		var args struct {
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return "error: 参数解析失败: " + err.Error()
		}
		return RunShell(args.Command, args.Timeout)
	}
	if result, ok := DispatchBuiltin(tc.Function.Name, tc.Function.Arguments); ok {
		return result
	}
	return "error: 未知工具 " + tc.Function.Name
}

func (a *Agent) buildMessages() []Message {
	msgs := make([]Message, 0, len(a.history)+1)
	msgs = append(msgs, Message{Role: "system", Content: a.systemPrompt()})
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
	t := estimateTokens(a.systemPrompt())
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
		tokenLine = fmt.Sprintf("token: %d（prompt %d / completion %d，API 实报）",
			a.lastUsage.TotalTokens, a.lastUsage.PromptTokens, a.lastUsage.CompletionTokens)
	} else {
		tokenLine = fmt.Sprintf("token: ~%d（本地估算）", a.totalTokens())
	}
	return fmt.Sprintf("%s\n消息: %d 条\n会话文件: %s", tokenLine, len(a.history), a.sessionPath)
}

func (a *Agent) PromptUsage() string {
	if a.lastUsage != nil {
		return formatTokens(a.lastUsage.PromptTokens)
	}
	return "~" + formatTokens(a.totalTokens())
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func (a *Agent) Model() string { return a.cfg.Model }

func (a *Agent) SetModel(m string) { a.cfg.Model = m }

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
		return fmt.Errorf("非法会话 id")
	}
	path := filepath.Join(a.sessionDir, id+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("会话不存在: %s", id)
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
	a.history = msgs
	a.sessionPath = path
	a.saved = len(msgs)
	return nil
}

type SessionInfo struct {
	ID      string
	ModTime time.Time
	Msgs    int
	Summary string
}

func (a *Agent) ListSessions() ([]SessionInfo, error) {
	entries, err := os.ReadDir(a.sessionDir)
	if err != nil {
		return nil, err
	}
	var list []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		si := SessionInfo{
			ID:      strings.TrimSuffix(e.Name(), ".jsonl"),
			ModTime: info.ModTime(),
		}
		if f, err := os.Open(filepath.Join(a.sessionDir, e.Name())); err == nil {
			dec := json.NewDecoder(f)
			for {
				var m Message
				if err := dec.Decode(&m); err != nil {
					break
				}
				si.Msgs++
				if si.Summary == "" && m.Role == "user" {
					s := strings.ReplaceAll(m.Content, "\n", " ")
					if utf8.RuneCountInString(s) > 30 {
						s = string([]rune(s)[:30]) + "..."
					}
					si.Summary = s
				}
			}
			f.Close()
		}
		list = append(list, si)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID > list[j].ID })
	return list, nil
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
	return []ToolDef{
		def("run_shell",
			"在 Linux bash 中执行 shell 命令，返回 stdout/stderr/退出码。读文件、搜索、运行程序等系统操作都用它。",
			`{"type":"object","properties":{"command":{"type":"string","description":"要执行的 bash 命令"},"timeout":{"type":"integer","description":"超时秒数，默认 60，最大 300"}},"required":["command"]}`),
		def("get_time",
			"获取当前日期时间（含时区）",
			`{"type":"object","properties":{}}`),
		def("get_env",
			"获取指定环境变量的值（疑似敏感的变量会被拒绝）",
			`{"type":"object","properties":{"names":{"type":"array","items":{"type":"string"},"description":"环境变量名列表"}},"required":["names"]}`),
		def("calc",
			"计算四则运算表达式，支持 + - * / % 与括号",
			`{"type":"object","properties":{"expression":{"type":"string","description":"算数表达式，如 (1+2)*3/4"}},"required":["expression"]}`),
	}
}
