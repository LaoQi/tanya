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
	"sync/atomic"
	"time"
)

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf(MsgAPIStatus, e.Status, e.Body)
}

func (c *Client) endpoint(path string) string {
	return strings.TrimSuffix(c.cfg.BaseURL, "/") + path
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	return req, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &httpError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	return resp, nil
}

var streamIdleTimeout = 60 * time.Second

type idleWatcher struct {
	feed   chan struct{}
	quit   chan struct{}
	cancel context.CancelFunc
	fired  atomic.Bool
}

func newIdleWatcher(ctx context.Context, d time.Duration) (*idleWatcher, context.Context) {
	ictx, cancel := context.WithCancel(ctx)
	w := &idleWatcher{feed: make(chan struct{}, 1), quit: make(chan struct{}), cancel: cancel}
	go func() {
		t := time.NewTimer(d)
		defer t.Stop()
		for {
			select {
			case <-w.quit:
				return
			case <-w.feed:
				if !t.Stop() {
					select {
					case <-t.C:
					default:
					}
				}
				t.Reset(d)
			case <-t.C:
				w.fired.Store(true)
				cancel()
				return
			}
		}
	}()
	return w, ictx
}

func (w *idleWatcher) Write(p []byte) (int, error) {
	select {
	case w.feed <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (w *idleWatcher) stop() {
	close(w.quit)
	w.cancel()
}

func (c *Client) streamSSE(ctx context.Context, path string, payload any, onData func(string) error) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	w, ictx := newIdleWatcher(ctx, streamIdleTimeout)
	defer w.stop()
	err = scanSSE(ictx, io.TeeReader(resp.Body, w), onData)
	if err != nil && w.fired.Load() {
		return fmt.Errorf(MsgStreamIdle, streamIdleTimeout)
	}
	return err
}

func scanSSE(ctx context.Context, r io.Reader, onData func(string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "[DONE]" {
			return nil
		}
		if err := onData(data); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf(MsgReadStream, err)
	}
	return nil
}
