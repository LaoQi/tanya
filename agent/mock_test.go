package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
}

type mockLLM struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	steps  []mockStep
	reqs   []chatRequest
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

func (m *mockLLM) config() *Config {
	cfg := defaultConfig()
	cfg.BaseURL = m.server.URL
	cfg.APIKey = "test-key"
	cfg.SessionDir = m.t.TempDir()
	return cfg
}
