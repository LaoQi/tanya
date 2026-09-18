package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

const agentModelListLimit = 50

const agentSessionListLimit = 20

var agentKeyNames = []string{"model", "reasoning_effort", "models", "usage", "stat", "sessions", "config_path"}

var agentWritableKeys = []string{"model", "reasoning_effort"}

func agentKeysLabel() string { return strings.Join(agentKeyNames, "、") }

func agentWritableLabel() string { return strings.Join(agentWritableKeys, "、") }

var agentDesc = "读取或修改当前 agent 的模型与思考等级，并查询运行态统计、可用模型与会话列表。" +
	"改动仅本次会话有效（不写入配置文件，进程退出即恢复），对下一次请求生效。" +
	"切换模型后 prompt cache 不复用，需重新预热。" +
	"action=get 按 key 读取；action=set 按 key 写入可写 key（需同时给 value）。" +
	"key 可用: " + agentKeysLabel() + "；可写 key: " + agentWritableLabel() + "。"

type configTarget interface {
	Model() string
	SetModel(string) error
	ReasoningEffort() string
	SetReasoningEffort(string) error
	ListModels() ([]string, error)
	ListSessions() ([]SessionInfo, error)
	ConfigPath() string
	NoSave() bool
	Stats() Stats
}

type agentTool struct{ target configTarget }

func newAgentTool(t configTarget) *agentTool { return &agentTool{target: t} }

func (t *agentTool) Name() string { return "agent_custom" }

func (t *agentTool) Definition() ToolDef {
	return newToolDef(t.Name(), agentDesc, agentParams())
}

type agentArgs struct {
	Action string  `json:"action"`
	Key    string  `json:"key"`
	Value  *string `json:"value"`
}

type keySpec struct {
	writable string
	read     func() string
	write    func(v string) (string, error)
}

func (t *agentTool) keys() map[string]keySpec {
	return map[string]keySpec{
		"model": {
			writable: MsgControlModelSpec,
			read:     func() string { return fmt.Sprintf(MsgControlModel, t.target.Model()) },
			write:    t.writeModel,
		},
		"reasoning_effort": {
			writable: MsgControlEffortSpec,
			read:     func() string { return fmt.Sprintf(MsgControlEffort, effortLabel(t.target.ReasoningEffort())) },
			write:    t.writeEffort,
		},
		"models":      {read: t.readModels},
		"usage":       {read: t.readUsage},
		"stat":        {read: t.readStat},
		"sessions":    {read: t.readSessions},
		"config_path": {read: t.readConfigPath},
	}
}

func (t *agentTool) Invoke(_ context.Context, argsJSON string) ToolResult {
	var args agentArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ToolResult{Text: fmt.Sprintf(MsgParseArgs, err)}
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	if action != "get" && action != "set" {
		return ToolResult{Text: fmt.Sprintf(MsgErrPrefix+MsgControlBadAction, args.Action)}
	}
	key := strings.ToLower(strings.TrimSpace(args.Key))
	if key == "" {
		return ToolResult{Text: fmt.Sprintf(MsgErrPrefix+MsgControlMissingKey, agentKeysLabel())}
	}
	spec, ok := t.keys()[key]
	if !ok {
		return ToolResult{Text: fmt.Sprintf(MsgErrPrefix+MsgControlBadKey, args.Key, agentKeysLabel())}
	}
	if action == "get" {
		return ToolResult{Text: spec.read()}
	}
	if spec.writable == "" {
		return ToolResult{Text: fmt.Sprintf(MsgErrPrefix+MsgControlReadOnlyKey, key, agentWritableLabel())}
	}
	if args.Value == nil {
		return ToolResult{Text: fmt.Sprintf(MsgErrPrefix+MsgControlNeedValue, spec.writable)}
	}
	out, err := spec.write(*args.Value)
	if err != nil {
		return ToolResult{Text: MsgErrPrefix + err.Error()}
	}
	return ToolResult{Text: out}
}

func (t *agentTool) writeModel(v string) (string, error) {
	old := t.target.Model()
	if err := t.target.SetModel(v); err != nil {
		return "", err
	}
	return fmt.Sprintf(MsgControlModelSwitch, old, t.target.Model()) + "\n" + MsgControlCacheHint, nil
}

func (t *agentTool) writeEffort(v string) (string, error) {
	old := effortLabel(t.target.ReasoningEffort())
	if err := t.target.SetReasoningEffort(v); err != nil {
		return "", err
	}
	return fmt.Sprintf(MsgControlEffortSwitch, old, effortLabel(t.target.ReasoningEffort())), nil
}

func (t *agentTool) readConfigPath() string {
	return fmt.Sprintf(MsgControlConfigPath, t.target.ConfigPath())
}

func (t *agentTool) readModels() string {
	models, err := t.target.ListModels()
	if err != nil {
		return MsgErrPrefix + err.Error()
	}
	if len(models) == 0 {
		return MsgControlModelsEmpty
	}
	var b strings.Builder
	fmt.Fprintf(&b, MsgControlModelsHead, len(models))
	for _, m := range models[:min(len(models), agentModelListLimit)] {
		fmt.Fprintf(&b, "\n  %s", m)
	}
	if len(models) > agentModelListLimit {
		fmt.Fprintf(&b, "\n"+MsgControlModelsTrim, agentModelListLimit, len(models))
	}
	return b.String()
}

func (t *agentTool) readUsage() string {
	st := t.target.Stats()
	if !st.HasContext {
		return MsgControlContextUnknown
	}
	out := fmt.Sprintf(MsgControlUsageContext, st.ContextTokens)
	if st.ContextTokens > 0 {
		rate := float64(st.ContextHit) / float64(st.ContextTokens) * 100
		out += "\n" + fmt.Sprintf(MsgControlUsageHit, st.ContextHit, fmt.Sprintf("%.2f%%", rate))
	}
	return out
}

func (t *agentTool) readStat() string {
	st := t.target.Stats()
	session := fmt.Sprintf(MsgControlStatSession, strings.TrimSuffix(filepath.Base(st.Session), ".jsonl"))
	switch {
	case st.Archived != "":
		session = fmt.Sprintf(MsgControlStatArchive, st.Archived)
	case t.target.NoSave():
		session = fmt.Sprintf(MsgControlStatSession, MsgControlStatNoSave)
	}
	return session + "\n" +
		fmt.Sprintf(MsgControlMessages, st.Messages) + "\n" +
		fmt.Sprintf(MsgControlTotals, st.PromptTokens, st.CompletionTokens, st.TotalTokens)
}

func (t *agentTool) readSessions() string {
	list, err := t.target.ListSessions()
	if err != nil {
		return MsgErrPrefix + err.Error()
	}
	if len(list) == 0 {
		return MsgControlSessionsEmpty
	}
	var b strings.Builder
	fmt.Fprintf(&b, MsgControlSessionsHead, len(list))
	for _, si := range list[:min(len(list), agentSessionListLimit)] {
		fmt.Fprintf(&b, "\n"+MsgControlSessionsLine, si.ID, si.Msgs, si.ModTime.Format("2006-01-02 15:04"), si.Path)
	}
	if len(list) > agentSessionListLimit {
		fmt.Fprintf(&b, "\n"+MsgControlSessionsTrim, len(list), agentSessionListLimit)
	}
	return b.String()
}

func effortLabel(v string) string {
	if v == "" {
		return MsgControlUnset
	}
	return v
}

func agentParams() string {
	keys, _ := json.Marshal(agentKeyNames)
	return fmt.Sprintf(`{"type":"object","properties":{`+
		`"action":{"type":"string","enum":["get","set"],"description":"get 读取 key 的当前值；set 写入可写 key（需同时给 value）"},`+
		`"key":{"type":"string","enum":%s,"description":"可写键 %s；只读键 models（服务端可用模型）、usage（上下文与缓存）、stat（会话统计）、sessions（会话列表与文件路径，jsonl 每行一条消息）、config_path（生效配置文件绝对路径，可用 run_shell 读取或修改，改动需重启生效）"},`+
		`"value":{"type":"string","description":"set 的新值（get 时忽略）。reasoning_effort 取 minimal/low/medium/high/max/off，off 表示清空该字段"}},`+
		`"required":["action","key"]}`, keys, agentWritableLabel())
}
