package style

import "testing"

func TestDocSkeleton(t *testing.T) {
	d := Doc(
		P(Dim.Text("a"), Span{Text: "b"}),
		RawText{Text: "raw output"},
		Rule{},
	)
	if len(d.Blocks) != 3 {
		t.Fatalf("blocks = %d", len(d.Blocks))
	}
	if _, ok := d.Blocks[1].(RawText); !ok {
		t.Error("RawText 应实现 Block")
	}
	p, ok := d.Blocks[0].(Paragraph)
	if !ok || len(p.Inlines) != 2 {
		t.Error("Paragraph 构造异常")
	}
	var _ Block = List{Items: []ListItem{{Blocks: []Block{P()}}}}
	var _ Inline = CodeSpan{Text: "x"}
	var _ Inline = SoftBreak{}
}
