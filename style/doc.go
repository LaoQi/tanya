package style

type Document struct {
	Blocks []Block
}

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

type RawText struct {
	Text string
}

type Span struct {
	Style Style
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
func (RawText) blockNode()   {}

func (Span) inlineNode()      {}
func (CodeSpan) inlineNode()  {}
func (SoftBreak) inlineNode() {}

func P(in ...Inline) Paragraph {
	return Paragraph{Inlines: in}
}

func Doc(blocks ...Block) Document {
	return Document{Blocks: blocks}
}
