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
	case ir.Table:
		return r.table(n)
	case ir.RawText:
		if strings.HasSuffix(n.Text, "\n") {
			return n.Text
		}
		return n.Text + "\n"
	}
	return ""
}

const (
	tableTopLeft     = "┌"
	tableTopMid      = "┬"
	tableTopRight    = "┐"
	tableMidLeft     = "├"
	tableMidMid      = "┼"
	tableMidRight    = "┤"
	tableBottomLeft  = "└"
	tableBottomMid   = "┴"
	tableBottomRight = "┘"
	tableH           = "─"
	tableV           = "│"
)

func (r Renderer) table(t ir.Table) string {
	pad := 1
	if t.Compact {
		pad = 0
	}
	sp := strings.Repeat(" ", pad)
	var b strings.Builder
	if t.Top {
		b.WriteString(r.tableRule(t.Widths, pad, tableTopLeft, tableTopMid, tableTopRight) + "\n")
	}
	for i, row := range t.Rows {
		b.WriteString(r.tableRow(row, t, sp) + "\n")
		if row.Header && (i < len(t.Rows)-1 || t.Bottom) {
			b.WriteString(r.tableRule(t.Widths, pad, tableMidLeft, tableMidMid, tableMidRight) + "\n")
		}
	}
	if t.Bottom {
		b.WriteString(r.tableRule(t.Widths, pad, tableBottomLeft, tableBottomMid, tableBottomRight) + "\n")
	}
	return b.String()
}

func (r Renderer) tableRule(widths []int, pad int, left, mid, right string) string {
	var b strings.Builder
	b.WriteString(left)
	for i, w := range widths {
		if i > 0 {
			b.WriteString(mid)
		}
		b.WriteString(strings.Repeat(tableH, w+2*pad))
	}
	b.WriteString(right)
	return r.tableBorder(b.String())
}

func (r Renderer) tableRow(row ir.TableRow, t ir.Table, sp string) string {
	var b strings.Builder
	for i := range t.Widths {
		b.WriteString(r.tableBorder(tableV))
		b.WriteString(sp)
		b.WriteString(r.tableCell(cellAt(row.Cells, i), row.Header, alignAt(t.Aligns, i), t.Widths[i]))
		b.WriteString(sp)
	}
	b.WriteString(r.tableBorder(tableV))
	return b.String()
}

func (r Renderer) tableBorder(s string) string {
	if seq := r.Theme.TableBorder.SGR(r.Prof); seq != "" {
		return seq + s + term.Reset
	}
	return s
}

func (r Renderer) tableCell(in []ir.Inline, header bool, align ir.Align, width int) string {
	if header {
		in = r.headInlines(in)
	}
	s := r.Inline(in...)
	w := term.Width(s)
	if w >= width {
		return s
	}
	fill := width - w
	switch align {
	case ir.AlignRight:
		return strings.Repeat(" ", fill) + s
	case ir.AlignCenter:
		l := fill / 2
		return strings.Repeat(" ", l) + s + strings.Repeat(" ", fill-l)
	default:
		return s + strings.Repeat(" ", fill)
	}
}

func (r Renderer) headInlines(in []ir.Inline) []ir.Inline {
	base := r.Theme.TableHead
	if base.SGR(r.Prof) == "" {
		return in
	}
	out := make([]ir.Inline, len(in))
	for i, node := range in {
		switch v := node.(type) {
		case ir.Span:
			st := v.Style
			if st.Fg.Kind == rstyle.KindNone {
				st.Fg = base.Fg
			}
			if st.Bg.Kind == rstyle.KindNone {
				st.Bg = base.Bg
			}
			st.Attr |= base.Attr
			out[i] = ir.Span{Style: st, Text: v.Text}
		default:
			out[i] = node
		}
	}
	return out
}

func alignAt(aligns []ir.Align, i int) ir.Align {
	if i < len(aligns) {
		return aligns[i]
	}
	return ir.AlignLeft
}

func cellAt(cells [][]ir.Inline, i int) []ir.Inline {
	if i < len(cells) {
		return cells[i]
	}
	return nil
}

func (r Renderer) itemInline(item ir.ListItem) string {
	for _, blk := range item.Blocks {
		if p, ok := blk.(ir.Paragraph); ok {
			return r.Inline(p.Inlines...)
		}
	}
	return ""
}
