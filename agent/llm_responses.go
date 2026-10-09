package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type responsesRequest struct {
	Model        string              `json:"model"`
	Instructions string              `json:"instructions,omitempty"`
	Input        []any               `json:"input"`
	Temperature  *float64            `json:"temperature,omitempty"`
	Reasoning    *responsesReasoning `json:"reasoning,omitempty"`
	Tools        []responsesTool     `json:"tools,omitempty"`
	Stream       bool                `json:"stream"`
	Store        bool                `json:"store"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type responsesContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesImagePart struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url"`
	Detail   string `json:"detail,omitempty"`
}

func responsesUserContent(m Message, detail string) []any {
	if !hasImages(m.Images) {
		return []any{responsesContentPart{Type: "input_text", Text: m.Content}}
	}
	parts := make([]any, 0, len(m.Images)+1)
	if m.Content != "" {
		parts = append(parts, responsesContentPart{Type: "input_text", Text: m.Content})
	}
	for _, img := range m.Images {
		url := img.URLValue()
		if url == "" {
			continue
		}
		parts = append(parts, responsesImagePart{Type: "input_image", ImageURL: url, Detail: img.DetailValue(detail)})
	}
	return parts
}

func buildResponsesInput(messages []Message, detail string) (string, []any) {
	var instructions string
	rest := messages
	if len(messages) > 0 && messages[0].Role == "system" {
		instructions = messages[0].Content
		rest = messages[1:]
	}
	items := make([]any, 0, len(rest))
	for _, m := range rest {
		switch m.Role {
		case "user":
			items = append(items, map[string]any{
				"type":    "message",
				"role":    "user",
				"content": responsesUserContent(m, detail),
			})
		case "assistant":
			for _, r := range m.ReasoningItems {
				if r.Content == "" {
					continue
				}
				item := map[string]any{
					"type":    "reasoning",
					"summary": []any{},
					"content": []responsesContentPart{
						{Type: "reasoning_text", Text: r.Content},
					},
				}
				if r.ID != "" {
					item["id"] = r.ID
				}
				items = append(items, item)
			}
			if m.Content != "" {
				items = append(items, map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []responsesContentPart{
						{Type: "output_text", Text: m.Content},
					},
				})
			}
			for _, tc := range m.ToolCalls {
				items = append(items, map[string]any{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				})
			}
		case "tool":
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": m.ToolCallID,
				"output":  m.Content,
			})
		}
	}
	return instructions, items
}

func responsesTools(defs []ToolDef) []responsesTool {
	tools := make([]responsesTool, 0, len(defs))
	for _, d := range defs {
		tools = append(tools, responsesTool{
			Type:        "function",
			Name:        d.Function.Name,
			Description: d.Function.Description,
			Parameters:  d.Function.Parameters,
		})
	}
	return tools
}

type responsesEvent struct {
	Type string `json:"type"`
}

type responsesTextDelta struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
}

type responsesContentText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesOutputItem struct {
	Type      string                 `json:"type"`
	ID        string                 `json:"id"`
	CallID    string                 `json:"call_id"`
	Name      string                 `json:"name"`
	Arguments string                 `json:"arguments"`
	Content   []responsesContentText `json:"content"`
}

type responsesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type responsesCompleted struct {
	Type     string `json:"type"`
	Response struct {
		Output []responsesOutputItem `json:"output"`
		Usage  *responsesUsage       `json:"usage"`
		Error  *responsesError       `json:"error"`
		Status string                `json:"status"`
	} `json:"response"`
}

type responsesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *Client) responsesStream(ctx context.Context, messages []Message, sink EventSink) (*Message, error) {
	instructions, input := buildResponsesInput(messages, c.cfg.ImageDetail)
	msg := &Message{Role: "assistant"}
	var usage *Usage
	var hasDelta bool
	start := time.Now()

	reasonOpen := false
	endReason := func() {
		if reasonOpen {
			reasonOpen = false
			sink.Emit(Event{Kind: EventReasoningEnd})
		}
	}

	err := c.streamSSE(ctx, "/responses", responsesRequest{
		Model:        c.cfg.Model,
		Instructions: instructions,
		Input:        input,
		Temperature:  temperatureParam(c.cfg),
		Reasoning:    reasoningParam(c.cfg.ReasoningEffort),
		Tools:        responsesTools(c.tools),
		Stream:       true,
	}, func(data string) error {
		var ev responsesEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return nil
		}
		switch ev.Type {
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			var d responsesTextDelta
			if json.Unmarshal([]byte(data), &d) != nil || d.Delta == "" {
				return nil
			}
			reasonOpen = true
			sink.Emit(Event{Kind: EventReasoning, Text: d.Delta})
		case "response.reasoning_text.done", "response.reasoning_summary_text.done":
			endReason()
		case "response.output_item.done":
			var d struct {
				Item struct {
					Type string `json:"type"`
				} `json:"item"`
			}
			if json.Unmarshal([]byte(data), &d) == nil && d.Item.Type == "reasoning" {
				endReason()
			}
		case "response.function_call_arguments.delta":
			var d struct {
				ItemID string `json:"item_id"`
				Delta  string `json:"delta"`
			}
			if json.Unmarshal([]byte(data), &d) != nil || d.Delta == "" {
				return nil
			}
			endReason()
			sink.Emit(Event{Kind: EventToolCall, ToolID: d.ItemID, ToolArgs: d.Delta})
		case "response.output_text.delta":
			var d responsesTextDelta
			if json.Unmarshal([]byte(data), &d) != nil || d.Delta == "" {
				return nil
			}
			endReason()
			hasDelta = true
			msg.Content += d.Delta
			sink.Emit(Event{Kind: EventContent, Text: d.Delta})
		case "response.completed", "response.incomplete":
			var cc responsesCompleted
			if json.Unmarshal([]byte(data), &cc) != nil {
				return nil
			}
			applyCompletedOutput(msg, &cc.Response.Output, hasDelta)
			if cc.Response.Usage != nil {
				usage = usageFromResponses(cc.Response.Usage)
			}
			if ev.Type == "response.completed" && cc.Response.Error != nil {
				return fmt.Errorf(MsgRespFailed, cc.Response.Error.Message)
			}
		case "response.failed":
			var cc responsesCompleted
			if json.Unmarshal([]byte(data), &cc) == nil && cc.Response.Error != nil {
				return fmt.Errorf(MsgRespFailed, cc.Response.Error.Message)
			}
			return fmt.Errorf(MsgRespFailed, "response.failed")
		case "error":
			var ee struct {
				Error *responsesError `json:"error"`
			}
			if json.Unmarshal([]byte(data), &ee) == nil && ee.Error != nil {
				return fmt.Errorf(MsgRespFailed, ee.Error.Message)
			}
		}
		return nil
	})
	if err != nil {
		var he *httpError
		if errors.As(err, &he) && he.Status == http.StatusNotFound {
			return nil, fmt.Errorf(MsgAPIStatus+"（%s）", he.Status, he.Body, MsgRespHint404)
		}
		return nil, err
	}
	endReason()
	msg.Usage = usage
	msg.Duration = time.Since(start)
	return msg, nil
}

func applyCompletedOutput(msg *Message, output *[]responsesOutputItem, hasDelta bool) {
	for _, item := range *output {
		switch item.Type {
		case "message":
			if hasDelta {
				continue
			}
			for _, part := range item.Content {
				if part.Type == "output_text" {
					msg.Content += part.Text
				}
			}
		case "function_call":
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   item.CallID,
				Type: "function",
			})
			tc := &msg.ToolCalls[len(msg.ToolCalls)-1]
			tc.Function.Name = item.Name
			tc.Function.Arguments = item.Arguments
		case "reasoning":
			msg.ReasoningItems = append(msg.ReasoningItems, ReasoningItem{
				ID:      item.ID,
				Content: reasoningText(item.Content),
			})
		}
	}
}

func reasoningText(parts []responsesContentText) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "reasoning_text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func usageFromResponses(u *responsesUsage) *Usage {
	out := &Usage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
	}
	if u.InputTokensDetails != nil {
		out.PromptTokensDetails = &promptTokensDetails{CachedTokens: u.InputTokensDetails.CachedTokens}
	}
	if u.OutputTokensDetails != nil {
		out.ReasoningTokens = u.OutputTokensDetails.ReasoningTokens
	}
	return out
}

func reasoningParam(effort string) *responsesReasoning {
	if effort == "" {
		return nil
	}
	return &responsesReasoning{Effort: effort}
}
