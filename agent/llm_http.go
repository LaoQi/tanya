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
	return scanSSE(ctx, resp.Body, onData)
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
