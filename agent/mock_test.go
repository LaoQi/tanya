package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type mockToolCall struct {
	id   string
	name string
	args string
}

type mockStep struct {
	status    int
	content   string
	toolCalls []mockToolCall
	usage     *Usage
	reasoning string
}

type mockLLM struct {
	t       *testing.T
	server  *httptest.Server
	mu      sync.Mutex
	steps   []mockStep
	reqs    []chatRequest
	rawReqs []map[string]any
}

func newMockLLM(t *testing.T, steps ...mockStep) *mockLLM {
	t.Helper()
	m := &mockLLM{t: t, steps: steps}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockLLM) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/responses") {
		m.handleResponses(w, r)
		return
	}
	m.handleChat(w, r)
}

func (m *mockLLM) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	if len(m.steps) == 0 {
		m.mu.Unlock()
		http.Error(w, "no more steps", http.StatusInternalServerError)
		return
	}
	step := m.steps[0]
	m.steps = m.steps[1:]
	m.mu.Unlock()

	if step.status != 0 && step.status != http.StatusOK {
		http.Error(w, "mock error", step.status)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	writeChunk := func(delta map[string]any) {
		b, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": delta}}})
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	if step.reasoning != "" {
		runes := []rune(step.reasoning)
		for i := 0; i < len(runes); i += 2 {
			end := i + 2
			if end > len(runes) {
				end = len(runes)
			}
			writeChunk(map[string]any{"reasoning_content": string(runes[i:end])})
		}
	}
	runes := []rune(step.content)
	for i := 0; i < len(runes); i += 2 {
		end := i + 2
		if end > len(runes) {
			end = len(runes)
		}
		writeChunk(map[string]any{"content": string(runes[i:end])})
	}
	for idx, tc := range step.toolCalls {
		writeChunk(map[string]any{"tool_calls": []map[string]any{{
			"index":    idx,
			"id":       tc.id,
			"type":     "function",
			"function": map[string]any{"name": tc.name, "arguments": ""},
		}}})
		argRunes := []rune(tc.args)
		for i := 0; i < len(argRunes); i += 3 {
			end := i + 3
			if end > len(argRunes) {
				end = len(argRunes)
			}
			writeChunk(map[string]any{"tool_calls": []map[string]any{{
				"index":    idx,
				"function": map[string]any{"arguments": string(argRunes[i:end])},
			}}})
		}
	}
	if step.usage != nil {
		b, _ := json.Marshal(map[string]any{"choices": []map[string]any{}, "usage": step.usage})
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (m *mockLLM) handleResponses(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	m.rawReqs = append(m.rawReqs, raw)
	if len(m.steps) == 0 {
		m.mu.Unlock()
		http.Error(w, "no more steps", http.StatusInternalServerError)
		return
	}
	step := m.steps[0]
	m.steps = m.steps[1:]
	m.mu.Unlock()

	if step.status != 0 && step.status != http.StatusOK {
		http.Error(w, "mock error", step.status)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	send := func(event string, payload map[string]any) {
		payload["type"] = event
		b, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	if step.reasoning != "" {
		runes := []rune(step.reasoning)
		for i := 0; i < len(runes); i += 2 {
			end := i + 2
			if end > len(runes) {
				end = len(runes)
			}
			send("response.reasoning_text.delta", map[string]any{"delta": string(runes[i:end])})
		}
	}
	runes := []rune(step.content)
	for i := 0; i < len(runes); i += 2 {
		end := i + 2
		if end > len(runes) {
			end = len(runes)
		}
		send("response.output_text.delta", map[string]any{"delta": string(runes[i:end])})
	}

	var output []map[string]any
	if step.reasoning != "" {
		output = append(output, map[string]any{
			"type":    "reasoning",
			"id":      "rs_test",
			"content": []map[string]any{{"type": "reasoning_text", "text": step.reasoning}},
		})
	}
	if step.content != "" {
		output = append(output, map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": step.content},
			},
		})
	}
	for idx, tc := range step.toolCalls {
		callID := tc.id
		if callID == "" {
			callID = fmt.Sprintf("call_%d", idx+1)
		}
		send("response.output_item.added", map[string]any{
			"item": map[string]any{
				"type": "function_call", "id": "fc_" + callID,
				"call_id": callID, "name": tc.name, "arguments": "",
			},
		})
		argRunes := []rune(tc.args)
		for i := 0; i < len(argRunes); i += 3 {
			end := i + 3
			if end > len(argRunes) {
				end = len(argRunes)
			}
			send("response.function_call_arguments.delta", map[string]any{
				"item_id": "fc_" + callID,
				"delta":   string(argRunes[i:end]),
			})
		}
		output = append(output, map[string]any{
			"type": "function_call", "id": "fc_" + callID,
			"call_id": callID, "name": tc.name, "arguments": tc.args,
		})
	}

	usagePayload := map[string]any{"output": output, "usage": usageToResponses(step.usage)}
	send("response.completed", map[string]any{"response": usagePayload})
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func usageToResponses(u *Usage) any {
	if u == nil {
		return nil
	}
	m := map[string]any{
		"input_tokens":  u.PromptTokens,
		"output_tokens": u.CompletionTokens,
		"total_tokens":  u.TotalTokens,
	}
	if u.PromptTokensDetails != nil {
		m["input_tokens_details"] = map[string]any{"cached_tokens": u.PromptTokensDetails.CachedTokens}
	}
	if u.ReasoningTokens > 0 {
		m["output_tokens_details"] = map[string]any{"reasoning_tokens": u.ReasoningTokens}
	}
	return m
}

func (m *mockLLM) config() *Config {
	cfg := defaultConfig()
	cfg.BaseURL = m.server.URL
	cfg.APIKey = "test-key"
	cfg.GlobalSession = m.t.TempDir()
	cfg.ApiProtocol = "chat"
	return cfg
}
