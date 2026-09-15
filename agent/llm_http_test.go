package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
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
