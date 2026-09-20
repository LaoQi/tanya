package ir

import rstyle "github.com/LaoQi/tanya/render/style"

type Block interface {
	blockNode()
}

type Inline interface {
	inlineNode()
}

type Paragraph struct {
	Inlines []Inline
}

type Heading struct {
	Level   int
	Inlines []Inline
}

type CodeBlock struct {
	Lang  string
	Lines []string
}

type List struct {
	Ordered bool
	Start   int
	Items   []ListItem
}

type ListItem struct {
	Blocks []Block
}

type Quote struct {
	Blocks []Block
}

type Rule struct{}

type Align uint8

const (
	AlignLeft Align = iota
	AlignRight
	AlignCenter
)

type TableRow struct {
	Cells  [][]Inline
	Header bool
}

type Table struct {
	Aligns  []Align
	Widths  []int
	Rows    []TableRow
	Top     bool
	Bottom  bool
	Compact bool
}

type RawText struct {
	Text string
}

type Span struct {
	Style rstyle.Style
	Text  string
}

type CodeSpan struct {
	Text string
}

type SoftBreak struct{}

func (Paragraph) blockNode() {}
func (Heading) blockNode()   {}
func (CodeBlock) blockNode() {}
func (List) blockNode()      {}
func (ListItem) blockNode()  {}
func (Quote) blockNode()     {}
func (Rule) blockNode()      {}
func (Table) blockNode()     {}
func (RawText) blockNode()   {}

func (Span) inlineNode()      {}
func (CodeSpan) inlineNode()  {}
func (SoftBreak) inlineNode() {}
