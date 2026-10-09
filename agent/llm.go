package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type ReasoningItem struct {
	ID      string `json:"id,omitempty"`
	Content string `json:"content,omitempty"`
}

type Message struct {
	Role             string          `json:"role"`
	Content          string          `json:"content,omitempty"`
	Images           []ImageRef      `json:"images,omitempty"`
	ToolCalls        []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ReasoningItems   []ReasoningItem `json:"reasoning_items,omitempty"`
	Usage            *Usage          `json:"-"`
	Duration         time.Duration   `json:"-"`
}

type chatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *chatImageURL `json:"image_url,omitempty"`
}

type chatImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheHitTokens   int `json:"prompt_cache_hit_tokens,omitempty"`
	CacheMissTokens  int `json:"prompt_cache_miss_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`

	PromptTokensDetails     *promptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type completionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

func (u *Usage) normalize() {
	if u.ReasoningTokens == 0 && u.CompletionTokensDetails != nil {
		u.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
}

func (u *Usage) CacheHit() int {
	if u.CacheHitTokens > 0 {
		return u.CacheHitTokens
	}
	if u.PromptTokensDetails != nil {
		return u.PromptTokensDetails.CachedTokens
	}
	return 0
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type Client struct {
	cfg   *Config
	http  *http.Client
	tools []ToolDef
}

func NewClient(cfg *Config, tools []ToolDef) *Client {
	return &Client{cfg: cfg, http: &http.Client{}, tools: tools}
}

type chatRequest struct {
	Model           string         `json:"model"`
	Messages        []any          `json:"messages"`
	Temperature     *float64       `json:"temperature,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	Tools           []ToolDef      `json:"tools,omitempty"`
	Stream          bool           `json:"stream"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

func (c *Client) ListModels() ([]string, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf(MsgAPIKey)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := c.newRequest(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf(MsgModelList, err)
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (c *Client) ChatStream(ctx context.Context, messages []Message, sink EventSink) (*Message, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf(MsgAPIKey)
	}
	if c.cfg.ApiProtocol == "chat" {
		return c.chatStream(ctx, messages, sink)
	}
	return c.responsesStream(ctx, messages, sink)
}

func temperatureParam(cfg *Config) *float64 {
	if cfg.ReasoningEffort != "" {
		return nil
	}
	return &cfg.Temperature
}

func chatWireMessages(messages []Message, detail string) []any {
	wire := make([]any, len(messages))
	for i, m := range messages {
		wm := m
		wm.ReasoningItems = nil
		wm.ReasoningContent = ""
		if m.Role == "assistant" {
			wm.ReasoningContent = joinReasoning(m.ReasoningItems)
		}
		if m.Role == "user" && hasImages(m.Images) {
			wire[i] = newChatPartsMessage(wm, m.Images, detail)
			continue
		}
		wm.Images = nil
		wire[i] = wm
	}
	return wire
}

func hasImages(images []ImageRef) bool {
	for _, img := range images {
		if img.Present() {
			return true
		}
	}
	return false
}

type chatPartsMessage struct {
	Role    string            `json:"role"`
	Content []chatContentPart `json:"content"`
	Name    string            `json:"name,omitempty"`
}

func newChatPartsMessage(m Message, images []ImageRef, detail string) chatPartsMessage {
	parts := make([]chatContentPart, 0, len(images)+1)
	if m.Content != "" {
		parts = append(parts, chatContentPart{Type: "text", Text: m.Content})
	}
	for _, img := range images {
		url := img.URLValue()
		if url == "" {
			continue
		}
		parts = append(parts, chatContentPart{
			Type:     "image_url",
			ImageURL: &chatImageURL{URL: url, Detail: img.DetailValue(detail)},
		})
	}
	return chatPartsMessage{Role: m.Role, Content: parts, Name: m.Name}
}

func joinReasoning(items []ReasoningItem) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range items {
		b.WriteString(r.Content)
	}
	return b.String()
}

func (c *Client) chatStream(ctx context.Context, messages []Message, sink EventSink) (*Message, error) {
	wire := chatWireMessages(messages, c.cfg.ImageDetail)
	msg := &Message{Role: "assistant"}
	var usage *Usage
	var reasoning strings.Builder
	type toolAcc struct {
		id, typ, name, args string
	}
	accs := map[int]*toolAcc{}
	start := time.Now()

	reasonOpen := false
	endReason := func() {
		if reasonOpen {
			reasonOpen = false
			sink.Emit(Event{Kind: EventReasoningEnd})
		}
	}

	err := c.streamSSE(ctx, "/chat/completions", chatRequest{
		Model:           c.cfg.Model,
		Messages:        wire,
		Temperature:     temperatureParam(c.cfg),
		ReasoningEffort: c.cfg.ReasoningEffort,
		Tools:           c.tools,
		Stream:          true,
		StreamOptions:   &streamOptions{IncludeUsage: true},
	}, func(data string) error {
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
			usage.normalize()
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.ReasoningContent != "" {
				reasonOpen = true
				reasoning.WriteString(ch.Delta.ReasoningContent)
				sink.Emit(Event{Kind: EventReasoning, Text: ch.Delta.ReasoningContent})
			}
			if ch.Delta.Content != "" {
				endReason()
				msg.Content += ch.Delta.Content
				sink.Emit(Event{Kind: EventContent, Text: ch.Delta.Content})
			}
			if len(ch.Delta.ToolCalls) > 0 {
				endReason()
			}
			for _, tc := range ch.Delta.ToolCalls {
				a := accs[tc.Index]
				if a == nil {
					a = &toolAcc{}
					accs[tc.Index] = a
				}
				if tc.ID != "" {
					a.id = tc.ID
				}
				if tc.Type != "" {
					a.typ = tc.Type
				}
				if tc.Function.Name != "" {
					a.name = tc.Function.Name
				}
				a.args += tc.Function.Arguments
				sink.Emit(Event{Kind: EventToolCall, ToolIndex: tc.Index, ToolID: tc.ID, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	endReason()
	if reasoning.Len() > 0 {
		msg.ReasoningItems = append(msg.ReasoningItems, ReasoningItem{Content: reasoning.String()})
	}
	msg.Usage = usage
	msg.Duration = time.Since(start)
	if len(accs) > 0 {
		idxs := make([]int, 0, len(accs))
		for i := range accs {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		for _, i := range idxs {
			a := accs[i]
			var tc ToolCall
			tc.ID = a.id
			tc.Type = a.typ
			if tc.Type == "" {
				tc.Type = "function"
			}
			tc.Function.Name = a.name
			tc.Function.Arguments = a.args
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
	}
	return msg, nil
}
