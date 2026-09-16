package render

import (
	"github.com/LaoQi/tanya/render/ir"
	rstyle "github.com/LaoQi/tanya/render/style"
	"github.com/LaoQi/tanya/render/theme"
	"strconv"
	"strings"

	"github.com/LaoQi/tanya/render/term"
)

func NewRenderer(prof term.Profile) Renderer {
	return NewThemedRenderer(prof, theme.DefaultTheme())
}

func NewThemedRenderer(prof term.Profile, th theme.Theme) Renderer {
	return Renderer{Prof: prof, Theme: th}
}

type Renderer struct {
	Prof  term.Profile
	Theme theme.Theme
}

func (r Renderer) Inline(in ...ir.Inline) string {
	var b strings.Builder
	var open rstyle.Style
	hasOpen := false
	for _, i := range in {
		switch n := i.(type) {
		case ir.Span:
			if hasOpen && n.Style == open {
				b.WriteString(n.Text)
				continue
			}
			if hasOpen {
				b.WriteString(term.Reset)
				hasOpen = false
			}
			if seq := n.Style.SGR(r.Prof); seq != "" {
				b.WriteString(seq)
				b.WriteString(n.Text)
				open = n.Style
				hasOpen = true
			} else {
				b.WriteString(n.Text)
			}
		case ir.CodeSpan:
			if hasOpen {
				b.WriteString(term.Reset)
				hasOpen = false
			}
			if seq := r.Theme.CodeInline.SGR(r.Prof); seq != "" {
				b.WriteString(seq)
				b.WriteString(n.Text)
				b.WriteString(term.Reset)
			} else {
				b.WriteString(n.Text)
			}
		case ir.SoftBreak:
			b.WriteString("\n")
		}
	}
	if hasOpen {
		b.WriteString(term.Reset)
	}
	return b.String()
}

func Sprint(in ...ir.Inline) string {
	return NewRenderer(term.GetProfile()).Inline(in...)
}

func (r Renderer) Block(b ir.Block) string {
	switch n := b.(type) {
	case ir.Paragraph:
		return r.Inline(n.Inlines...) + "\n"
	case ir.Heading:
		body := r.Inline(n.Inlines...)
		if seq := r.Theme.Heading(n.Level).SGR(r.Prof); seq != "" {
			return seq + body + term.Reset + "\n"
		}
		return body + "\n"
	case ir.CodeBlock:
		var b strings.Builder
		for _, l := range n.Lines {
			b.WriteString(r.Inline(ir.Span{Style: r.Theme.CodeBlock, Text: l}))
			b.WriteString("\n")
		}
		return b.String()
	case ir.List:
		var b strings.Builder
		for i, item := range n.Items {
			prefix := r.Theme.Bullet
			if n.Ordered {
				prefix = strconv.Itoa(n.Start+i) + ". "
			}
			b.WriteString(prefix)
			b.WriteString(r.itemInline(item))
			b.WriteString("\n")
		}
		return b.String()
	case ir.Quote:
		var b strings.Builder
		for _, blk := range n.Blocks {
			b.WriteString(r.Theme.QuotePrefix)
			if p, ok := blk.(ir.Paragraph); ok {
				b.WriteString(r.Inline(p.Inlines...))
			}
			b.WriteString("\n")
		}
		return b.String()
	case ir.Rule:
		return r.Theme.Rule + "\n"
	case ir.RawText:
		if strings.HasSuffix(n.Text, "\n") {
			return n.Text
		}
		return n.Text + "\n"
	}
	return ""
}

func (r Renderer) itemInline(item ir.ListItem) string {
	for _, blk := range item.Blocks {
		if p, ok := blk.(ir.Paragraph); ok {
			return r.Inline(p.Inlines...)
		}
	}
	return ""
}
