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

type interactiveTool interface {
	Interactive(argsJSON string) bool
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

func newToolDef(name, desc, params string) ToolDef {
	var t ToolDef
	t.Type = "function"
	t.Function.Name = name
	t.Function.Description = desc
	t.Function.Parameters = json.RawMessage(params)
	return t
}

func interactiveOf(t Tool, argsJSON string) bool {
	it, ok := t.(interactiveTool)
	if !ok {
		return false
	}
	return it.Interactive(argsJSON)
}

func allTools(shell *shellTool) []Tool {
	return append([]Tool{shell}, builtinTools()...)
}
