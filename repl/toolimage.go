package repl

import (
	"fmt"
	"strings"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/term"
)

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
