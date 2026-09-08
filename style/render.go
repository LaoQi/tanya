package style

import (
	"strconv"
	"strings"
)

type Theme struct {
	Headings    [6]Style
	CodeBlock   Style
	CodeInline  Style
	QuotePrefix string
	Bullet      string
	Rule        string
}

func DefaultTheme() Theme {
	return Theme{
		Headings: [6]Style{
			{Attr: AttrBold, Fg: Color16(15)},
			{Attr: AttrBold, Fg: Color16(14)},
			{Attr: AttrBold, Fg: Color16(12)},
			{Fg: Color16(7)},
			{Fg: Color16(8)},
			{Fg: Color16(8)},
		},
		CodeBlock:   Style{Fg: Color16(8)},
		CodeInline:  Style{Fg: Color16(10)},
		QuotePrefix: "▌ ",
		Bullet:      "• ",
		Rule:        "────",
	}
}

func NewRenderer(prof Profile) Renderer {
	return NewThemedRenderer(prof, DefaultTheme())
}

func NewThemedRenderer(prof Profile, theme Theme) Renderer {
	return Renderer{Prof: prof, Theme: theme}
}

func (t Theme) heading(level int) Style {
	if level < 1 || level > 6 {
		return Style{}
	}
	return t.Headings[level-1]
}

type Renderer struct {
	Prof  Profile
	Theme Theme
}

func (r Renderer) Inline(in ...Inline) string {
	var b strings.Builder
	var open Style
	hasOpen := false
	for _, i := range in {
		switch n := i.(type) {
		case Span:
			if hasOpen && n.Style == open {
				b.WriteString(n.Text)
				continue
			}
			if hasOpen {
				b.WriteString(resetSequence)
				hasOpen = false
			}
			if seq := r.Prof.sgr(n.Style); seq != "" {
				b.WriteString(seq)
				b.WriteString(n.Text)
				open = n.Style
				hasOpen = true
			} else {
				b.WriteString(n.Text)
			}
		case CodeSpan:
			if hasOpen {
				b.WriteString(resetSequence)
				hasOpen = false
			}
			if seq := r.Prof.sgr(r.Theme.CodeInline); seq != "" {
				b.WriteString(seq)
				b.WriteString(n.Text)
				b.WriteString(resetSequence)
			} else {
				b.WriteString(n.Text)
			}
		case SoftBreak:
			b.WriteString("\n")
		}
	}
	if hasOpen {
		b.WriteString(resetSequence)
	}
	return b.String()
}

func Sprint(in ...Inline) string {
	return NewRenderer(current).Inline(in...)
}

func (r Renderer) Block(b Block) string {
	switch n := b.(type) {
	case Paragraph:
		return r.Inline(n.Inlines...) + "\n"
	case Heading:
		body := r.Inline(n.Inlines...)
		if seq := r.Prof.sgr(r.Theme.heading(n.Level)); seq != "" {
			return seq + body + resetSequence + "\n"
		}
		return body + "\n"
	case CodeBlock:
		var b strings.Builder
		for _, l := range n.Lines {
			b.WriteString(r.Inline(r.Theme.CodeBlock.Text(l)))
			b.WriteString("\n")
		}
		return b.String()
	case List:
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
	case Quote:
		var b strings.Builder
		for _, blk := range n.Blocks {
			b.WriteString(r.Theme.QuotePrefix)
			if p, ok := blk.(Paragraph); ok {
				b.WriteString(r.Inline(p.Inlines...))
			}
			b.WriteString("\n")
		}
		return b.String()
	case Rule:
		return r.Theme.Rule + "\n"
	case RawText:
		if strings.HasSuffix(n.Text, "\n") {
			return n.Text
		}
		return n.Text + "\n"
	}
	return ""
}

func (r Renderer) itemInline(item ListItem) string {
	for _, blk := range item.Blocks {
		if p, ok := blk.(Paragraph); ok {
			return r.Inline(p.Inlines...)
		}
	}
	return ""
}

func (r Renderer) Doc(d Document) string {
	var b strings.Builder
	for _, blk := range d.Blocks {
		b.WriteString(r.Block(blk))
	}
	return b.String()
}
