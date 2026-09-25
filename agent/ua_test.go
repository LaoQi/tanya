package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserAgentHeader(t *testing.T) {
	var ua, modelUA string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		modelUA = r.Header.Get("User-Agent")
		fmt.Fprint(w, `{"data":[{"id":"m1"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg := defaultConfig()
	cfg.BaseURL = srv.URL + "/v1"
	cfg.APIKey = "test-key"
	cfg.ApiProtocol = "chat"
	c := NewClient(cfg, nil)
	if _, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListModels(); err != nil {
		t.Fatal(err)
	}
	if ua != DefaultUserAgent || modelUA != DefaultUserAgent {
		t.Errorf("UA 头异常: chat=%q models=%q", ua, modelUA)
	}
}
