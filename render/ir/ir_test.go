package ir

import (
	"testing"

	rstyle "github.com/LaoQi/tanyan/render/style"
)

func TestIRSkeleton(t *testing.T) {
	blocks := []Block{
		Paragraph{Inlines: []Inline{Span{Style: rstyle.Style{Attr: rstyle.AttrBold}, Text: "a"}, Span{Text: "b"}}},
		RawText{Text: "raw output"},
		Rule{},
		List{Items: []ListItem{{Blocks: []Block{Paragraph{}}}}},
		Quote{Blocks: []Block{Paragraph{}}},
		CodeBlock{Lines: []string{"x"}},
		Heading{Level: 1},
	}
	if len(blocks) != 7 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	if _, ok := blocks[1].(RawText); !ok {
		t.Error("RawText 应实现 Block")
	}
	p, ok := blocks[0].(Paragraph)
	if !ok || len(p.Inlines) != 2 {
		t.Error("Paragraph 构造异常")
	}
	var _ Inline = CodeSpan{Text: "x"}
	var _ Inline = SoftBreak{}
	var _ Block = ListItem{}
}
