package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type ReasoningItem struct {
	ID      string `json:"id"`
	Content string `json:"content,omitempty"`
}

type Message struct {
	Role           string          `json:"role"`
	Content        string          `json:"content,omitempty"`
	ToolCalls      []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID     string          `json:"tool_call_id,omitempty"`
	Name           string          `json:"name,omitempty"`
	ReasoningItems []ReasoningItem `json:"reasoning_items,omitempty"`
	Usage          *Usage          `json:"-"`
	Stat           *RequestStat    `json:"-"`
}

type RequestStat struct {
	Duration time.Duration
	TTFT     time.Duration
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheHitTokens   int `json:"prompt_cache_hit_tokens,omitempty"`
	CacheMissTokens  int `json:"prompt_cache_miss_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`

	PromptTokensDetails *promptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
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
	cfg  *Config
	http *http.Client
}

func NewClient(cfg *Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{}}
}

type chatRequest struct {
	Model           string         `json:"model"`
	Messages        []Message      `json:"messages"`
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
			Content   string `json:"content"`
			ToolCalls []struct {
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
	url := strings.TrimSuffix(c.cfg.BaseURL, "/") + "/models"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf(MsgAPIStatus, resp.StatusCode, strings.TrimSpace(string(b)))
	}
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

func (c *Client) ChatStream(ctx context.Context, messages []Message, onDelta func(string)) (*Message, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf(MsgAPIKey)
	}
	if c.cfg.ApiProtocol == "chat" {
		return c.chatStream(ctx, messages, onDelta)
	}
	return c.responsesStream(ctx, messages, onDelta)
}

func temperatureParam(cfg *Config) *float64 {
	if cfg.ReasoningEffort != "" {
		return nil
	}
	return &cfg.Temperature
}

func (c *Client) chatStream(ctx context.Context, messages []Message, onDelta func(string)) (*Message, error) {
	wire := make([]Message, len(messages))
	copy(wire, messages)
	for i := range wire {
		wire[i].ReasoningItems = nil
	}
	body, err := json.Marshal(chatRequest{
		Model:           c.cfg.Model,
		Messages:        wire,
		Temperature:     temperatureParam(c.cfg),
		ReasoningEffort: c.cfg.ReasoningEffort,
		Tools:           ToolDefs(),
		Stream:          true,
		StreamOptions:   &streamOptions{IncludeUsage: true},
	})
	if err != nil {
		return nil, err
	}
	url := strings.TrimSuffix(c.cfg.BaseURL, "/") + "/chat/completions"
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
		return nil, fmt.Errorf(MsgAPIStatus, resp.StatusCode, strings.TrimSpace(string(b)))
	}

	msg := &Message{Role: "assistant"}
	var ttft time.Duration
	var usage *Usage
	type toolAcc struct {
		id, typ, name, args string
	}
	accs := map[int]*toolAcc{}

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
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if ttft == 0 && (len(chunk.Choices) > 0 || chunk.Usage != nil) {
			ttft = time.Since(start)
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				msg.Content += ch.Delta.Content
				if onDelta != nil {
					onDelta(ch.Delta.Content)
				}
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
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf(MsgReadStream, err)
	}
	msg.Usage = usage
	msg.Stat = &RequestStat{Duration: time.Since(start), TTFT: ttft}

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
