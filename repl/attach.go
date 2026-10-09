package repl

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/term"
)

const (
	MaxImageURLLen = 8192
	sniffLen       = 512
)

type AttachOptions struct {
	Workspace func() string
	MaxBytes  int
	MaxCount  int
}

func ParseAttachments(line string, opt AttachOptions) ([]agent.ImageRef, error) {
	tokens := attachTokens(line)
	if len(tokens) == 0 {
		return nil, nil
	}
	max := opt.MaxCount
	if max <= 0 {
		max = agent.DefaultImageMaxCount
	}
	var out []agent.ImageRef
	for _, tok := range tokens {
		img, err := attachOne(tok, opt)
		if err != nil {
			return nil, err
		}
		if img == nil {
			continue
		}
		if len(out) >= max {
			return nil, fmt.Errorf(MsgImageTooMany, max)
		}
		out = append(out, *img)
	}
	return out, nil
}

type attachSpan struct {
	text       string
	start, end int
}

func attachSpans(line string) []attachSpan {
	var out []attachSpan
	for i := 0; i < len(line); {
		if line[i] != '@' || (i > 0 && !isAttachSpace(line[i-1])) {
			i++
			continue
		}
		j := i + 1
		if j < len(line) && (line[j] == '"' || line[j] == '\'') {
			quote := line[j]
			k := j + 1
			for k < len(line) && line[k] != quote {
				k++
			}
			if k >= len(line) {
				i = j
				continue
			}
			out = append(out, attachSpan{text: line[j+1 : k], start: i, end: k + 1})
			i = k + 1
			continue
		}
		k := j
		for k < len(line) && !isAttachSpace(line[k]) {
			k++
		}
		if k > j {
			out = append(out, attachSpan{text: line[j:k], start: i, end: k})
		}
		i = k
	}
	return out
}

func attachTokens(line string) []string {
	spans := attachSpans(line)
	if len(spans) == 0 {
		return nil
	}
	out := make([]string, 0, len(spans))
	for _, sp := range spans {
		out = append(out, sp.text)
	}
	return out
}

func stripAttachTokens(line string) string {
	var b strings.Builder
	prev := 0
	for _, sp := range attachSpans(line) {
		b.WriteString(line[prev:sp.start])
		prev = sp.end
	}
	b.WriteString(line[prev:])
	return b.String()
}

func PrepareContent(line string, opt AttachOptions) (agent.Content, error) {
	imgs, err := ParseAttachments(line, opt)
	if err != nil {
		return agent.Content{}, err
	}
	if len(imgs) > 0 && strings.TrimSpace(stripAttachTokens(line)) == "" {
		return agent.Content{}, errors.New(MsgImageNoText)
	}
	return agent.Content{Text: line, Images: imgs}, nil
}

func imagesText(images []agent.ImageRef) string {
	if len(images) == 0 {
		return ""
	}
	parts := make([]string, 0, len(images))
	for _, img := range images {
		parts = append(parts, fmt.Sprintf(MsgImageItemFmt, term.OneLine(img.Name), formatBytes(int64(img.Bytes))))
	}
	return fmt.Sprintf(MsgImageListFmt, strings.Join(parts, MsgImageSep))
}

func isAttachSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func attachOne(tok string, opt AttachOptions) (*agent.ImageRef, error) {
	switch {
	case strings.HasPrefix(tok, "data:"):
		return attachDataURL(tok, opt)
	case strings.HasPrefix(tok, "http://"), strings.HasPrefix(tok, "https://"):
		if len(tok) > MaxImageURLLen {
			return nil, fmt.Errorf(MsgImageURLTooLong, MaxImageURLLen)
		}
		return &agent.ImageRef{URL: tok, Name: urlName(tok)}, nil
	}
	path := resolveAttachPath(tok, opt)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	max := opt.MaxBytes
	if max <= 0 {
		max = agent.DefaultImageMaxBytes
	}
	if info.Size() > int64(max) {
		return nil, fmt.Errorf(MsgImageTooLarge, filepath.Base(path), max)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	mime := SniffImageMIME(data)
	if mime == "" {
		return nil, fmt.Errorf(MsgImageBadFormat, filepath.Base(path))
	}
	w, h := ImageDimensions(data, mime)
	return &agent.ImageRef{
		MIME:   mime,
		Data:   base64.StdEncoding.EncodeToString(data),
		Name:   filepath.Base(path),
		Bytes:  len(data),
		Width:  w,
		Height: h,
	}, nil
}

func attachDataURL(tok string, opt AttachOptions) (*agent.ImageRef, error) {
	comma := strings.IndexByte(tok, ',')
	if comma < 0 || !strings.HasSuffix(tok[:comma], ";base64") {
		return nil, fmt.Errorf(MsgImageBadFormat, "data URL")
	}
	meta := strings.TrimSuffix(tok[5:comma], ";base64")
	mime := normImageMIME(meta)
	if mime == "" {
		return nil, fmt.Errorf(MsgImageBadFormat, "data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(tok[comma+1:])
	if err != nil {
		return nil, fmt.Errorf(MsgImageBadFormat, "data URL")
	}
	max := opt.MaxBytes
	if max <= 0 {
		max = agent.DefaultImageMaxBytes
	}
	if len(raw) > max {
		return nil, fmt.Errorf(MsgImageTooLarge, "data URL", max)
	}
	if SniffImageMIME(raw) == "" {
		return nil, fmt.Errorf(MsgImageBadFormat, "data URL")
	}
	w, h := ImageDimensions(raw, mime)
	return &agent.ImageRef{MIME: mime, Data: tok[comma+1:], Name: "data-url", Bytes: len(raw), Width: w, Height: h}, nil
}

func resolveAttachPath(p string, opt AttachOptions) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if p == "~" {
				return home
			}
			p = filepath.Join(home, p[2:])
		}
	}
	if !filepath.IsAbs(p) {
		base := ""
		if opt.Workspace != nil {
			base = opt.Workspace()
		}
		if base != "" {
			p = filepath.Join(base, p)
		}
	}
	return filepath.Clean(p)
}

func urlName(u string) string {
	s := u
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return "url"
	}
	return s
}

func normImageMIME(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg":
		return "image/jpeg"
	case "image/png":
		return "image/png"
	case "image/gif":
		return "image/gif"
	case "image/webp":
		return "image/webp"
	}
	return ""
}

func SniffImageMIME(data []byte) string {
	head := data
	if len(head) > sniffLen {
		head = head[:sniffLen]
	}
	return normImageMIME(http.DetectContentType(head))
}

func ImageDimensions(data []byte, mime string) (int, int) {
	if mime == "image/webp" {
		return webpDimensions(data)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func webpDimensions(data []byte) (int, int) {
	if len(data) < 21 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0
	}
	switch string(data[12:16]) {
	case "VP8X":
		if len(data) < 30 {
			return 0, 0
		}
		w := int(data[24]) | int(data[25])<<8 | int(data[26])<<16
		h := int(data[27]) | int(data[28])<<8 | int(data[29])<<16
		return w + 1, h + 1
	case "VP8 ":
		if len(data) < 27 || data[20] != 0x9d || data[21] != 0x01 || data[22] != 0x2a {
			return 0, 0
		}
		return (int(data[23]) | int(data[24])<<8) & 0x3fff, (int(data[25]) | int(data[26])<<8) & 0x3fff
	case "VP8L":
		if len(data) < 25 || data[20] != 0x2f {
			return 0, 0
		}
		b := uint32(data[21]) | uint32(data[22])<<8 | uint32(data[23])<<16 | uint32(data[24])<<24
		return int(b&0x3fff) + 1, int(b>>14&0x3fff) + 1
	}
	return 0, 0
}
