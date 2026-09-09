package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func buildResponsesInput(messages []Message) (string, []any) {
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
				"type": "message",
				"role": "user",
				"content": []responsesContentPart{
					{Type: "input_text", Text: m.Content},
				},
			})
		case "assistant":
			for _, r := range m.ReasoningItems {
				if r.Content == "" {
					continue
				}
				items = append(items, map[string]any{
					"type": "reasoning",
					"id":   r.ID,
					"content": []responsesContentPart{
						{Type: "reasoning_text", Text: r.Content},
					},
				})
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

func responsesTools() []responsesTool {
	defs := ToolDefs()
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
	instructions, input := buildResponsesInput(messages)
	body, err := json.Marshal(responsesRequest{
		Model:        c.cfg.Model,
		Instructions: instructions,
		Input:        input,
		Temperature:  temperatureParam(c.cfg),
		Reasoning:    reasoningParam(c.cfg.ReasoningEffort),
		Tools:        responsesTools(),
		Stream:       true,
	})
	if err != nil {
		return nil, err
	}
	url := strings.TrimSuffix(c.cfg.BaseURL, "/") + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", c.cfg.UserAgent)

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		text := strings.TrimSpace(string(b))
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf(MsgAPIStatus+"（%s）", resp.StatusCode, text, MsgRespHint404)
		}
		return nil, fmt.Errorf(MsgAPIStatus, resp.StatusCode, text)
	}

	msg := &Message{Role: "assistant"}
	var firstEvent, firstReasoning, firstContent time.Duration
	var usage *Usage
	var hasDelta bool

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "[DONE]" {
			break
		}
		var ev responsesEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if firstEvent == 0 {
			firstEvent = time.Since(start)
		}
		switch ev.Type {
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			var d responsesTextDelta
			if json.Unmarshal([]byte(data), &d) != nil || d.Delta == "" {
				continue
			}
			if firstReasoning == 0 {
				firstReasoning = time.Since(start)
			}
			sink.Emit(Event{Kind: EventReasoning, Text: d.Delta})
		case "response.function_call_arguments.delta":
			var d struct {
				ItemID string `json:"item_id"`
				Delta  string `json:"delta"`
			}
			if json.Unmarshal([]byte(data), &d) != nil || d.Delta == "" {
				continue
			}
			sink.Emit(Event{Kind: EventToolCall, ToolID: d.ItemID, ToolArgs: d.Delta})
		case "response.output_text.delta":
			var d responsesTextDelta
			if json.Unmarshal([]byte(data), &d) != nil || d.Delta == "" {
				continue
			}
			if firstContent == 0 {
				firstContent = time.Since(start)
			}
			hasDelta = true
			msg.Content += d.Delta
			sink.Emit(Event{Kind: EventContent, Text: d.Delta})
		case "response.completed", "response.incomplete":
			var cc responsesCompleted
			if json.Unmarshal([]byte(data), &cc) != nil {
				continue
			}
			applyCompletedOutput(msg, &cc.Response.Output, hasDelta)
			if cc.Response.Usage != nil {
				usage = usageFromResponses(cc.Response.Usage)
				sink.Emit(Event{Kind: EventUsage, Usage: usage})
			}
			if ev.Type == "response.completed" && cc.Response.Error != nil {
				return nil, fmt.Errorf(MsgRespFailed, cc.Response.Error.Message)
			}
		case "response.failed":
			var cc responsesCompleted
			if json.Unmarshal([]byte(data), &cc) == nil && cc.Response.Error != nil {
				return nil, fmt.Errorf(MsgRespFailed, cc.Response.Error.Message)
			}
			return nil, fmt.Errorf(MsgRespFailed, "response.failed")
		case "error":
			var ee struct {
				Error *responsesError `json:"error"`
			}
			if json.Unmarshal([]byte(data), &ee) == nil && ee.Error != nil {
				return nil, fmt.Errorf(MsgRespFailed, ee.Error.Message)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf(MsgReadStream, err)
	}
	msg.Usage = usage
	msg.Stat = &RequestStat{Duration: time.Since(start), FirstEvent: firstEvent, FirstReasoning: firstReasoning, FirstContent: firstContent}
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
