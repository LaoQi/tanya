package style

import (
	"strings"
	"unicode/utf8"
)

type groupKind uint8

const (
	groupNone groupKind = iota
	groupFence
	groupList
	groupQuote
)

type MarkdownBuf struct {
	pending     strings.Builder
	lines       []string
	kind        groupKind
	fenceLang   string
	listOrdered bool
	closed      []Block
}

func NewMarkdownBuf() *MarkdownBuf { return &MarkdownBuf{} }

func (b *MarkdownBuf) Reset() { *b = MarkdownBuf{} }

func (b *MarkdownBuf) Write(delta string) []Block {
	b.pending.WriteString(delta)
	b.drain()
	out := b.closed
	b.closed = nil
	return out
}

func (b *MarkdownBuf) Flush() []Block { return b.Write("") }

func (b *MarkdownBuf) Close() []Block {
	b.drain()
	switch b.kind {
	case groupFence:
		raw := "```" + b.fenceLang
		for _, l := range b.lines {
			raw += "\n" + l
		}
		b.closed = append(b.closed, RawText{Text: raw})
	case groupList:
		b.closed = append(b.closed, b.buildList())
	case groupQuote:
		b.closed = append(b.closed, b.buildQuote())
	}
	b.kind = groupNone
	b.lines = nil
	if b.pending.Len() > 0 {
		p := cleanLine(b.pending.String())
		b.pending.Reset()
		if p != "" {
			b.closed = append(b.closed, Paragraph{Inlines: ParseInline(p)})
		}
	}
	out := b.closed
	b.closed = nil
	return out
}

func (b *MarkdownBuf) drain() {
	p := b.pending.String()
	for {
		idx := strings.IndexByte(p, '\n')
		if idx < 0 {
			break
		}
		b.feedLine(p[:idx])
		p = p[idx+1:]
	}
	b.pending.Reset()
	b.pending.WriteString(p)
}

func (b *MarkdownBuf) feedLine(line string) {
	line = cleanLine(line)
	trimmed := strings.TrimSpace(line)
	if b.kind == groupFence {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			b.closed = append(b.closed, CodeBlock{Lang: b.fenceLang, Lines: b.lines})
			b.lines = nil
			b.kind = groupNone
			b.fenceLang = ""
		} else {
			b.lines = append(b.lines, line)
		}
		return
	}
	if trimmed == "" {
		b.closeGroup()
		b.closed = append(b.closed, Paragraph{})
		return
	}
	if strings.HasPrefix(line, "```") {
		b.closeGroup()
		b.kind = groupFence
		b.fenceLang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
		return
	}
	if lvl := headingLevel(line); lvl > 0 {
		b.closeGroup()
		b.closed = append(b.closed, Heading{Level: lvl, Inlines: ParseInline(strings.TrimSpace(line[lvl+1:]))})
		return
	}
	if isRule(trimmed) {
		b.closeGroup()
		b.closed = append(b.closed, Rule{})
		return
	}
	if item, ordered, ok := listItem(line); ok {
		if b.kind != groupList {
			b.closeGroup()
			b.kind = groupList
			b.listOrdered = ordered
			b.lines = nil
		}
		b.lines = append(b.lines, item)
		return
	}
	if q, ok := quoteLine(line); ok {
		if b.kind != groupQuote {
			b.closeGroup()
			b.kind = groupQuote
			b.lines = nil
		}
		b.lines = append(b.lines, q)
		return
	}
	b.closeGroup()
	b.closed = append(b.closed, Paragraph{Inlines: ParseInline(line)})
}

func (b *MarkdownBuf) closeGroup() {
	switch b.kind {
	case groupList:
		b.closed = append(b.closed, b.buildList())
	case groupQuote:
		b.closed = append(b.closed, b.buildQuote())
	}
	b.kind = groupNone
	b.lines = nil
}

func (b *MarkdownBuf) buildList() Block {
	items := make([]ListItem, len(b.lines))
	for i, l := range b.lines {
		items[i] = ListItem{Blocks: []Block{Paragraph{Inlines: ParseInline(l)}}}
	}
	return List{Ordered: b.listOrdered, Start: 1, Items: items}
}

func (b *MarkdownBuf) buildQuote() Block {
	blocks := make([]Block, len(b.lines))
	for i, l := range b.lines {
		blocks[i] = Paragraph{Inlines: ParseInline(l)}
	}
	return Quote{Blocks: blocks}
}

func cleanLine(s string) string {
	s = Strip(s)
	var b strings.Builder
	for _, r := range s {
		if r == '\r' || (r < 0x20 && r != '\t') || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func headingLevel(line string) int {
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i > 6 {
		return 0
	}
	if i == len(line) || line[i] != ' ' {
		return 0
	}
	return i
}

func isRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != c {
			return false
		}
	}
	return true
}

func listItem(line string) (string, bool, bool) {
	if len(line) >= 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		return strings.TrimSpace(line[2:]), false, true
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i > 0 && i < len(line) && (line[i] == '.' || line[i] == ')') && i+1 < len(line) && line[i+1] == ' ' {
		return strings.TrimSpace(line[i+2:]), true, true
	}
	return "", false, false
}

func quoteLine(line string) (string, bool) {
	if strings.HasPrefix(line, ">") {
		rest := strings.TrimPrefix(line[1:], " ")
		return strings.TrimSpace(rest), true
	}
	return "", false
}

func ParseInline(s string) []Inline {
	var out []Inline
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Span{Text: buf.String()})
			buf.Reset()
		}
	}
	i := 0
	for i < len(s) {
		switch c := s[i]; {
		case c == '`':
			end := strings.IndexByte(s[i+1:], '`')
			if end > 0 {
				flush()
				out = append(out, CodeSpan{Text: s[i+1 : i+1+end]})
				i += end + 2
				continue
			}
			buf.WriteByte(c)
			i++
		case c == '*' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "**")
			if end > 0 {
				flush()
				out = append(out, Span{Style: Style{Attr: AttrBold}, Text: s[i+2 : i+2+end]})
				i += end + 4
				continue
			}
			buf.WriteString("**")
			i += 2
		case c == '*':
			end := strings.IndexByte(s[i+1:], '*')
			if end > 0 && s[i+1] != ' ' && s[i+end] != ' ' {
				flush()
				out = append(out, Span{Style: Style{Attr: AttrItalic}, Text: s[i+1 : i+1+end]})
				i += end + 2
				continue
			}
			buf.WriteByte(c)
			i++
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			buf.WriteRune(r)
			i += size
		}
	}
	flush()
	return out
}
