package agent

import (
	"math"
	"strings"
)

type ImageRef struct {
	MIME   string `json:"mime,omitempty"`
	Data   string `json:"data,omitempty"`
	URL    string `json:"url,omitempty"`
	Name   string `json:"name,omitempty"`
	Bytes  int    `json:"bytes,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Detail string `json:"detail,omitempty"`
}

var ImageDetails = []string{"low", "high", "original", "auto"}

func NormalizeImageDetail(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, d := range ImageDetails {
		if v == d {
			return d
		}
	}
	return ""
}

func (r ImageRef) DetailValue(fallback string) string {
	if d := NormalizeImageDetail(r.Detail); d != "" {
		return d
	}
	return NormalizeImageDetail(fallback)
}

func (r ImageRef) URLValue() string {
	if r.Data != "" {
		mime := r.MIME
		if mime == "" {
			mime = "image/png"
		}
		return "data:" + mime + ";base64," + r.Data
	}
	return r.URL
}

func (r ImageRef) Present() bool { return r.URLValue() != "" }

const (
	DefaultImageMaxBytes = 10 << 20
	DefaultImageMaxCount = 4
)

const (
	imageTokensMax   = 1024
	imagePixelLow    = 544 * 544
	imagePixelHigh   = 1300 * 1300
	imagePixelPerTok = float64(imagePixelHigh) / float64(imageTokensMax)
)

func estimateImageTokens(w, h int) int {
	if w <= 0 || h <= 0 {
		return imageTokensMax
	}
	px := float64(w) * float64(h)
	switch {
	case px < imagePixelLow:
		px = imagePixelLow
	case px > imagePixelHigh:
		px = imagePixelHigh
	}
	n := int(math.Round(px / imagePixelPerTok))
	if n > imageTokensMax {
		n = imageTokensMax
	}
	if n < 1 {
		n = 1
	}
	return n
}
