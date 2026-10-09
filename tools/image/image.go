package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaoQi/tanya/agent"
)

type Config struct {
	Workspace func() string
	Home      string
	MaxBytes  int
	Resize    bool
	Detail    string
}

type Tool struct {
	workspace func() string
	home      string
	maxBytes  int
	resize    bool
	detail    string
}

func New(cfg Config) *Tool {
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = agent.DefaultImageMaxBytes
	}
	return &Tool{
		workspace: cfg.Workspace,
		home:      cfg.Home,
		maxBytes:  maxBytes,
		resize:    cfg.Resize,
		detail:    agent.NormalizeImageDetail(cfg.Detail),
	}
}

func (t *Tool) Name() string { return "read_image" }

func (t *Tool) Definition() agent.ToolDef {
	return agent.NewToolDef(t.Name(), toolDesc(), readImageParams())
}

type readArgs struct {
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

func (t *Tool) Invoke(_ context.Context, argsJSON string) agent.ToolResult {
	var args readArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return agent.ToolResult{Text: fmt.Sprintf(agent.MsgParseArgs, err)}
	}
	res, err := t.read(args)
	if err != nil {
		return agent.ToolResult{Text: agent.MsgErrPrefix + err.Error()}
	}
	return res
}

func (t *Tool) read(args readArgs) (agent.ToolResult, error) {
	path := strings.TrimSpace(args.Path)
	if path == "" {
		return agent.ToolResult{}, fmt.Errorf(MsgImagePathEmpty)
	}
	detail := t.detail
	if raw := strings.TrimSpace(args.Detail); raw != "" {
		d := agent.NormalizeImageDetail(raw)
		if d == "" {
			return agent.ToolResult{}, fmt.Errorf(MsgBadDetail, raw)
		}
		detail = d
	}
	full := t.resolvePath(path)
	info, err := os.Stat(full)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf(MsgImageNotFound, path)
	}
	if !info.Mode().IsRegular() {
		return agent.ToolResult{}, fmt.Errorf(MsgImageNotFile, path)
	}
	if info.Size() > int64(t.maxBytes) {
		return agent.ToolResult{}, fmt.Errorf(MsgImageTooLarge, filepath.Base(full), t.maxBytes)
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf(MsgImageReadFail, path, err)
	}
	mime := agent.SniffImageMIME(raw)
	if mime == "" {
		return agent.ToolResult{}, fmt.Errorf(MsgImageBadFormat, filepath.Base(full))
	}
	data := agent.MaybeResizeImage(raw, mime, detail, t.resize)
	w, h := agent.ImageDimensions(data, mime)
	name := filepath.Base(full)
	ref := agent.ImageRef{
		MIME:   mime,
		Data:   base64.StdEncoding.EncodeToString(data),
		Name:   name,
		Bytes:  len(data),
		Width:  w,
		Height: h,
		Detail: detail,
	}
	return agent.ToolResult{Text: fmt.Sprintf(MsgReadImageFmt, name, dimensionsText(w, h), formatBytes(len(data))), Images: []agent.ImageRef{ref}}, nil
}

func (t *Tool) resolvePath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home := t.home
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		if p == "~" {
			return filepath.Clean(home)
		}
		return filepath.Clean(filepath.Join(home, p[2:]))
	}
	if !filepath.IsAbs(p) && t.workspace != nil {
		if base := t.workspace(); base != "" {
			return filepath.Clean(filepath.Join(base, p))
		}
	}
	return filepath.Clean(p)
}

func dimensionsText(w, h int) string {
	if w <= 0 || h <= 0 {
		return MsgImageSizeUnknown
	}
	return fmt.Sprintf("%d×%d", w, h)
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fk", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}
