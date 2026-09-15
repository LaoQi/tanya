package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type configTarget interface {
	Model() string
	SetModel(string) error
	ReasoningEffort() string
	SetReasoningEffort(string) error
	ListModels() ([]string, error)
	Stats() Stats
}

type agentTool struct{ target configTarget }

func newAgentTool(t configTarget) *agentTool { return &agentTool{target: t} }

func (t *agentTool) Name() string { return "agent_custom" }

func (t *agentTool) Definition() ToolDef {
	return newToolDef(t.Name(), agentDesc, agentParams())
}

type agentArgs struct {
	Action          string `json:"action"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
}

func (t *agentTool) Invoke(_ context.Context, argsJSON string) ToolResult {
	var args agentArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ToolResult{Text: fmt.Sprintf(MsgParseArgs, err)}
	}
	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "get":
		return ToolResult{Text: t.getText()}
	case "set":
		return ToolResult{Text: t.setText(args)}
	case "list_models":
		return ToolResult{Text: t.listModelsText()}
	default:
		return ToolResult{Text: fmt.Sprintf(MsgErrPrefix+MsgControlBadAction, args.Action)}
	}
}

func (t *agentTool) getText() string {
	var b strings.Builder
	fmt.Fprintf(&b, MsgControlModel+"\n", t.target.Model())
	fmt.Fprintf(&b, MsgControlEffort+"\n", effortLabel(t.target.ReasoningEffort()))
	st := t.target.Stats()
	if st.HasContext && st.ContextTokens > 0 {
		fmt.Fprintf(&b, MsgControlContext+"\n", st.ContextTokens, st.ContextHit, cacheRateLabel(st))
	} else {
		b.WriteString(MsgControlContextUnknown + "\n")
	}
	fmt.Fprintf(&b, MsgControlMessages, st.Messages)
	return b.String()
}

func (t *agentTool) setText(args agentArgs) string {
	provided := args.Model != ""
	model := strings.TrimSpace(args.Model)
	raw := strings.TrimSpace(args.ReasoningEffort)
	if !provided && raw == "" {
		return MsgControlNoChange
	}
	if provided && model == "" {
		return MsgErrPrefix + MsgEmptyModel
	}
	setEffort := raw != ""
	if setEffort && !strings.EqualFold(raw, "off") && normalizeEffort(raw) == "" {
		return fmt.Sprintf(MsgErrPrefix+MsgBadEffort, raw)
	}
	var lines []string
	if model != "" {
		old := t.target.Model()
		if err := t.target.SetModel(model); err != nil {
			return MsgErrPrefix + err.Error()
		}
		lines = append(lines, fmt.Sprintf(MsgControlModelSwitch, old, model))
	}
	if setEffort {
		old := effortLabel(t.target.ReasoningEffort())
		if err := t.target.SetReasoningEffort(raw); err != nil {
			return MsgErrPrefix + err.Error()
		}
		lines = append(lines, fmt.Sprintf(MsgControlEffortSwitch, old, effortLabel(effortValue(raw))))
	}
	if model != "" {
		lines = append(lines, MsgControlCacheHint)
	}
	return strings.Join(lines, "\n")
}

const agentModelListLimit = 50

func (t *agentTool) listModelsText() string {
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

func effortLabel(v string) string {
	if v == "" {
		return MsgControlUnset
	}
	return v
}

func effortValue(raw string) string {
	if strings.EqualFold(raw, "off") {
		return ""
	}
	return raw
}

func cacheRateLabel(st Stats) string {
	if st.ContextTokens <= 0 {
		return MsgControlNoCache
	}
	return fmt.Sprintf("%.2f%%", float64(st.ContextHit)/float64(st.ContextTokens)*100)
}

const agentDesc = "读取或修改当前 agent 的模型与思考等级，并查询运行态统计与可用模型。" +
	"改动仅本次会话有效（不写入配置文件，进程退出即恢复），对下一次请求生效。" +
	"切换模型后 prompt cache 不复用，需重新预热。" +
	"action=get 读取现状；set 修改（至少指定 model 或 reasoning_effort 之一）；list_models 查询服务端可用模型（网络请求，最长 10s）。"

func agentParams() string {
	levels, _ := json.Marshal(append(append([]string{}, EffortLevels...), "off"))
	return fmt.Sprintf(`{"type":"object","properties":{`+
		`"action":{"type":"string","enum":["get","set","list_models"],"description":"get 读取当前可调项与运行态；set 修改；list_models 查询服务端可用模型（网络请求，最长 10s，需 api_key）"},`+
		`"model":{"type":"string","description":"set 时指定新模型名，缺省表示不改此项"},`+
		`"reasoning_effort":{"type":"string","enum":%s,"description":"set 时指定思考等级，off 表示不发送该字段"}},`+
		`"required":["action"]}`, levels)
}
