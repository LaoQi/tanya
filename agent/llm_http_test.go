package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read boom") }

func TestScanSSE(t *testing.T) {
	t.Run("collects data lines until done", func(t *testing.T) {
		var got []string
		err := scanSSE(context.Background(),
			strings.NewReader(": keep-alive\n\nevent: x\ndata: a\ndata: b\n\ndata: [DONE]\n\ndata: after\n\n"),
			func(s string) error {
				got = append(got, s)
				return nil
			})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, ",") != "a,b" {
			t.Errorf("应只收集 [DONE] 前的 data 行: %v", got)
		}
	})

	t.Run("propagates handler error", func(t *testing.T) {
		sentinel := errors.New("boom")
		err := scanSSE(context.Background(), strings.NewReader("data: a\n\n"), func(string) error {
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("handler 错误应透传: %v", err)
		}
	})

	t.Run("aborts on canceled ctx", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var calls int
		err := scanSSE(ctx, strings.NewReader("data: a\n\ndata: b\n\n"), func(string) error {
			calls++
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("已取消的 ctx 应返回 context.Canceled: %v", err)
		}
		if calls != 0 {
			t.Errorf("取消后不应再处理 data: calls=%d", calls)
		}
	})

	t.Run("wraps scanner error", func(t *testing.T) {
		err := scanSSE(context.Background(), failingReader{}, func(string) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "读取流失败") {
			t.Fatalf("scanner 错误应包 MsgReadStream: %v", err)
		}
	})
}

func TestStreamSSEErrors(t *testing.T) {
	t.Run("marshal failure", func(t *testing.T) {
		c := NewClient(defaultConfig(), nil)
		payload := struct {
			Bad json.RawMessage `json:"bad"`
		}{Bad: json.RawMessage(`{invalid`)}
		if err := c.streamSSE(context.Background(), "/x", payload, func(string) error { return nil }); err == nil {
			t.Fatal("非法 payload 应返回 marshal 错误")
		}
	})

	t.Run("request construction failure", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.BaseURL = "://bad"
		c := NewClient(cfg, nil)
		if err := c.streamSSE(context.Background(), "/x", chatRequest{}, func(string) error { return nil }); err == nil {
			t.Fatal("非法 BaseURL 应返回构造错误")
		}
	})
}

func TestStreamSSEIdleTimeout(t *testing.T) {
	old := streamIdleTimeout
	streamIdleTimeout = 120 * time.Millisecond
	t.Cleanup(func() { streamIdleTimeout = old })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(2 * time.Second)
		fmt.Fprint(w, "data: late\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c := NewClient(&Config{BaseURL: srv.URL, APIKey: "k"}, nil)
	var got []string
	err := c.streamSSE(context.Background(), "/chat/completions", map[string]any{}, func(s string) error {
		got = append(got, s)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "空闲超时") {
		t.Fatalf("空闲应报超时错误: %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("空闲超时不应误判为中断: %v", err)
	}
	if len(got) != 1 || got[0] != "first" {
		t.Errorf("超时前的数据应已消费: %v", got)
	}
}

func TestStreamSSEIdleReset(t *testing.T) {
	old := streamIdleTimeout
	streamIdleTimeout = 400 * time.Millisecond
	t.Cleanup(func() { streamIdleTimeout = old })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 4; i++ {
			fmt.Fprintf(w, "data: %d\n\n", i)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(150 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c := NewClient(&Config{BaseURL: srv.URL, APIKey: "k"}, nil)
	var got []string
	err := c.streamSSE(context.Background(), "/chat/completions", map[string]any{}, func(s string) error {
		got = append(got, s)
		return nil
	})
	if err != nil {
		t.Fatalf("持续喂狗的流不应超时: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("应收完 4 条: %v", got)
	}
}
