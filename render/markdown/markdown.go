package markdown

import (
	"github.com/LaoQi/tanya/render/ir"
	rstyle "github.com/LaoQi/tanya/render/style"
	"strings"
	"unicode/utf8"

	"github.com/LaoQi/tanya/render/term"
)

const (
	fenceLineLimit   = 2000
	fenceByteLimit   = 256 << 10
	pendingByteLimit = 64 << 10
)

type groupKind uint8

const (
	groupNone groupKind = iota
	groupFence
	groupList
	groupQuote
	groupTable
)

type MarkdownBuf struct {
	pending     strings.Builder
	lines       []string
	kind        groupKind
	fenceLang   string
	fenceBytes  int
	listOrdered bool
	closed      []ir.Block

	width        int
	hasCand      bool
	candLine     string
	candCells    []string
	tableHead    [][]ir.Inline
	tableAligns  []ir.Align
	tableWidths  []int
	tableStarted bool
}

func NewMarkdownBuf() *MarkdownBuf { return &MarkdownBuf{} }

func (b *MarkdownBuf) SetWidth(cols int) { b.width = cols }

func (b *MarkdownBuf) Reset() { *b = MarkdownBuf{} }

func (b *MarkdownBuf) Write(delta string) []ir.Block {
	b.pending.WriteString(delta)
	b.drain()
	out := b.closed
	b.closed = nil
	return out
}

func (b *MarkdownBuf) Close() []ir.Block {
	b.drain()
	switch b.kind {
	case groupFence:
		raw := "```" + b.fenceLang
		for _, l := range b.lines {
			raw += "\n" + l
		}
		b.closed = append(b.closed, ir.RawText{Text: raw})
	case groupList:
		b.closed = append(b.closed, b.buildList())
	case groupQuote:
		b.closed = append(b.closed, b.buildQuote())
	case groupTable:
		b.closeTable()
	}
	b.flushCand()
	b.kind = groupNone
	b.lines = nil
	b.fenceLang = ""
	b.fenceBytes = 0
	if b.pending.Len() > 0 {
		p := cleanLine(b.pending.String())
		b.pending.Reset()
		if p != "" {
			b.closed = append(b.closed, ir.Paragraph{Inlines: ParseInline(p)})
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
	if len(p) > pendingByteLimit {
		b.flushPending(p)
		return
	}
	b.pending.WriteString(p)
}

// flushPending 把超出阈值的无换行缓冲就地降级输出：围栏内并入代码行，其余作为段落，
// 使畸形（或超长）输入的影响止于当前行，后续 delta 立即恢复流式解析。
func (b *MarkdownBuf) flushPending(p string) {
	if b.kind == groupFence {
		b.lines = append(b.lines, cleanLine(p))
		b.fenceBytes += len(p) + 1
		if len(b.lines) > fenceLineLimit || b.fenceBytes > fenceByteLimit {
			b.closeFence()
		}
		return
	}
	if b.kind == groupTable {
		b.closeTable()
	}
	b.closeGroup()
	if line := cleanLine(p); line != "" {
		b.closed = append(b.closed, ir.Paragraph{Inlines: ParseInline(line)})
	}
}

func (b *MarkdownBuf) feedLine(line string) {
	line = cleanLine(line)
	trimmed := strings.TrimSpace(line)
	if b.kind == groupFence {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			b.closeFence()
			return
		}
		b.lines = append(b.lines, line)
		b.fenceBytes += len(line) + 1
		if len(b.lines) > fenceLineLimit || b.fenceBytes > fenceByteLimit {
			b.closeFence()
		}
		return
	}
	if b.hasCand {
		if aligns, ok := parseTableSep(line); ok && len(aligns) == len(b.candCells) {
			b.openTable(aligns)
			return
		}
		b.flushCand()
	}
	if b.kind == groupTable {
		if cells, ok := splitRow(line); ok {
			b.feedTableRow(cellsToInline(cells))
			return
		}
		b.closeTable()
	}
	if trimmed == "" {
		b.closeGroup()
		b.closed = append(b.closed, ir.Paragraph{})
		return
	}
	if strings.HasPrefix(line, "```") {
		b.closeGroup()
		b.kind = groupFence
		b.fenceLang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
		b.fenceBytes = 0
		return
	}
	if lvl := headingLevel(line); lvl > 0 {
		b.closeGroup()
		b.closed = append(b.closed, ir.Heading{Level: lvl, Inlines: ParseInline(strings.TrimSpace(line[lvl+1:]))})
		return
	}
	if isRule(trimmed) {
		b.closeGroup()
		b.closed = append(b.closed, ir.Rule{})
		return
	}
	if !hasBlockPrefix(line) {
		if cells, ok := splitRow(line); ok {
			b.closeGroup()
			b.hasCand = true
			b.candLine = line
			b.candCells = cells
			return
		}
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
	b.closed = append(b.closed, ir.Paragraph{Inlines: ParseInline(line)})
}

func (b *MarkdownBuf) closeFence() {
	b.closed = append(b.closed, ir.CodeBlock{Lang: b.fenceLang, Lines: b.lines})
	b.lines = nil
	b.kind = groupNone
	b.fenceLang = ""
	b.fenceBytes = 0
}

func (b *MarkdownBuf) openTable(aligns []ir.Align) {
	b.kind = groupTable
	b.tableAligns = aligns
	b.tableHead = cellsToInline(b.candCells)
	b.tableWidths = measureCells(b.tableHead)
	b.tableStarted = false
	b.hasCand = false
	b.candLine = ""
	b.candCells = nil
}

func (b *MarkdownBuf) closeTable() {
	if b.kind != groupTable {
		return
	}
	blk := ir.Table{Aligns: b.tableAligns, Widths: b.tableWidths, Compact: b.compact(), Bottom: true}
	if !b.tableStarted {
		blk.Top = true
		blk.Rows = []ir.TableRow{{Cells: b.tableHead, Header: true}}
	}
	b.closed = append(b.closed, blk)
	b.kind = groupNone
	b.lines = nil
	b.tableHead = nil
	b.tableAligns = nil
	b.tableWidths = nil
	b.tableStarted = false
}

func (b *MarkdownBuf) flushCand() {
	if !b.hasCand {
		return
	}
	line := b.candLine
	b.hasCand = false
	b.candLine = ""
	b.candCells = nil
	if line != "" {
		b.closed = append(b.closed, ir.Paragraph{Inlines: ParseInline(line)})
	}
}

func (b *MarkdownBuf) feedTableRow(cells [][]ir.Inline) {
	cells = fitCells(cells, len(b.tableHead))
	blk := ir.Table{Aligns: b.tableAligns}
	if b.tableStarted {
		blk.Rows = []ir.TableRow{{Cells: cells}}
	} else {
		b.tableStarted = true
		b.tableWidths = growWidths(b.tableWidths, cells)
		blk.Top = true
		blk.Rows = []ir.TableRow{{Cells: b.tableHead, Header: true}, {Cells: cells}}
	}
	blk.Widths = b.tableWidths
	blk.Compact = b.compact()
	b.closed = append(b.closed, blk)
}

func (b *MarkdownBuf) compact() bool {
	if b.width <= 0 || len(b.tableWidths) == 0 {
		return false
	}
	loose := len(b.tableWidths) + 1 + 2*len(b.tableWidths)
	for _, w := range b.tableWidths {
		loose += w
	}
	return loose > b.width
}

func measureCells(cells [][]ir.Inline) []int {
	w := make([]int, len(cells))
	for i, c := range cells {
		w[i] = inlineWidth(c)
	}
	return w
}

func growWidths(widths []int, cells [][]ir.Inline) []int {
	for i, c := range cells {
		if i >= len(widths) {
			break
		}
		if n := inlineWidth(c); n > widths[i] {
			widths[i] = n
		}
	}
	return widths
}

func inlineWidth(in []ir.Inline) int {
	n := 0
	for _, node := range in {
		switch v := node.(type) {
		case ir.Span:
			n += term.Width(v.Text)
		case ir.CodeSpan:
			n += term.Width(v.Text)
		}
	}
	return n
}

func cellsToInline(cells []string) [][]ir.Inline {
	out := make([][]ir.Inline, len(cells))
	for i, c := range cells {
		if c != "" {
			out[i] = ParseInline(c)
		}
	}
	return out
}

func fitCells(cells [][]ir.Inline, n int) [][]ir.Inline {
	if len(cells) == n {
		return cells
	}
	out := make([][]ir.Inline, n)
	copy(out, cells)
	return out
}

func splitRow(line string) ([]string, bool) {
	s := strings.TrimSpace(line)
	if !strings.Contains(s, "|") {
		return nil, false
	}
	if strings.HasPrefix(s, "|") {
		s = s[1:]
	}
	if n := len(s); n > 0 && s[n-1] == '|' && (n < 2 || s[n-2] != '\\') {
		s = s[:n-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if s[i] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	if len(cells) == 0 {
		return nil, false
	}
	return cells, true
}

func parseTableSep(line string) ([]ir.Align, bool) {
	cells, ok := splitRow(line)
	if !ok {
		return nil, false
	}
	aligns := make([]ir.Align, len(cells))
	for i, c := range cells {
		if c == "" {
			return nil, false
		}
		left := strings.HasPrefix(c, ":")
		right := strings.HasSuffix(c, ":")
		body := strings.Trim(c, ":")
		if body == "" || strings.Trim(body, "-") != "" {
			return nil, false
		}
		switch {
		case left && right:
			aligns[i] = ir.AlignCenter
		case right:
			aligns[i] = ir.AlignRight
		default:
			aligns[i] = ir.AlignLeft
		}
	}
	return aligns, true
}

func hasBlockPrefix(line string) bool {
	if strings.HasPrefix(line, ">") {
		return true
	}
	_, _, ok := listItem(line)
	return ok
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

func (b *MarkdownBuf) buildList() ir.Block {
	items := make([]ir.ListItem, len(b.lines))
	for i, l := range b.lines {
		items[i] = ir.ListItem{Blocks: []ir.Block{ir.Paragraph{Inlines: ParseInline(l)}}}
	}
	return ir.List{Ordered: b.listOrdered, Start: 1, Items: items}
}

func (b *MarkdownBuf) buildQuote() ir.Block {
	blocks := make([]ir.Block, len(b.lines))
	for i, l := range b.lines {
		blocks[i] = ir.Paragraph{Inlines: ParseInline(l)}
	}
	return ir.Quote{Blocks: blocks}
}

func cleanLine(s string) string {
	s = term.Strip(s)
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

func ParseInline(s string) []ir.Inline {
	var out []ir.Inline
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, ir.Span{Text: buf.String()})
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
				out = append(out, ir.CodeSpan{Text: s[i+1 : i+1+end]})
				i += end + 2
				continue
			}
			buf.WriteByte(c)
			i++
		case c == '*' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "**")
			if end > 0 {
				flush()
				out = append(out, ir.Span{Style: rstyle.Style{Attr: rstyle.AttrBold}, Text: s[i+2 : i+2+end]})
				i += end + 4
				continue
			}
			buf.WriteString("**")
			i += 2
		case c == '*':
			end := strings.IndexByte(s[i+1:], '*')
			if end > 0 && s[i+1] != ' ' && s[i+end] != ' ' {
				flush()
				out = append(out, ir.Span{Style: rstyle.Style{Attr: rstyle.AttrItalic}, Text: s[i+1 : i+1+end]})
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
