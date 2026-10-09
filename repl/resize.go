package repl

import "github.com/LaoQi/tanya/agent"

const (
	ImageResizeLowSide = agent.ImageResizeLowSide
	ImageResizeSide    = agent.ImageResizeSide
)

func ResizeTargetSide(detail string) int { return agent.ResizeTargetSide(detail) }

func MaybeResizeImage(data []byte, mime, detail string, enabled bool) []byte {
	return agent.MaybeResizeImage(data, mime, detail, enabled)
}

func jpegOrientation(data []byte) int { return agent.JPEGOrientation(data) }

func SniffImageMIME(data []byte) string { return agent.SniffImageMIME(data) }

func ImageDimensions(data []byte, mime string) (int, int) {
	return agent.ImageDimensions(data, mime)
}

func webpDimensions(data []byte) (int, int) { return agent.WebPDimensions(data) }

func normImageMIME(mime string) string { return agent.NormalizeImageMIME(mime) }
