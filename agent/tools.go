package agent

import (
	"context"
	"encoding/json"
)

type Tool interface {
	Name() string
	Definition() ToolDef
	Invoke(ctx context.Context, argsJSON string) ToolResult
}

type Interactive interface {
	Interactive(argsJSON string) bool
}

type ToolResult struct {
	Text string
	Meta any
}

type toolRegistry struct {
	tools []Tool
}

func newToolRegistry(tools ...Tool) *toolRegistry {
	return &toolRegistry{tools: tools}
}

func (r *toolRegistry) defs() []ToolDef {
	defs := make([]ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}

func (r *toolRegistry) lookup(name string) (Tool, bool) {
	for _, t := range r.tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

func NewToolDef(name, desc, params string) ToolDef {
	var t ToolDef
	t.Type = "function"
	t.Function.Name = name
	t.Function.Description = desc
	t.Function.Parameters = json.RawMessage(params)
	return t
}

func interactiveOf(t Tool, argsJSON string) bool {
	it, ok := t.(Interactive)
	if !ok {
		return false
	}
	return it.Interactive(argsJSON)
}

type funcTool struct {
	name   string
	desc   string
	params string
	fn     func(ctx context.Context, argsJSON string) ToolResult
}

func (f funcTool) Name() string { return f.name }

func (f funcTool) Definition() ToolDef {
	return NewToolDef(f.name, f.desc, f.params)
}

func (f funcTool) Invoke(ctx context.Context, argsJSON string) ToolResult {
	return f.fn(ctx, argsJSON)
}

func NewTool(name, desc, params string, fn func(ctx context.Context, argsJSON string) ToolResult) Tool {
	return funcTool{name: name, desc: desc, params: params, fn: fn}
}

func assembleTools(registered []Tool, ctl configTarget) []Tool {
	out := make([]Tool, 0, len(registered)+1)
	out = append(out, registered...)
	return append(out, newAgentTool(ctl))
}
