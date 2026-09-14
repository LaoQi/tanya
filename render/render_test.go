package render

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanyan/render/ir"
	"github.com/LaoQi/tanyan/render/term"
)

func TestRenderBlockProfiles(t *testing.T) {
	doc := []ir.Block{
		ir.Heading{Level: 1, Inlines: []ir.Inline{ir.Span{Text: "标题"}}},
		ir.CodeBlock{Lang: "go", Lines: []string{"x := 1"}},
		ir.List{Ordered: false, Items: []ir.ListItem{{Blocks: []ir.Block{ir.Paragraph{Inlines: []ir.Inline{ir.Span{Text: "项"}}}}}}},
		ir.Quote{Blocks: []ir.Block{ir.Paragraph{Inlines: []ir.Inline{ir.Span{Text: "引"}}}}},
		ir.Rule{},
		ir.RawText{Text: "raw"},
	}
	colored := renderBlocks(t, term.Profile{TTY: true, Colors: term.Level16}, doc)
	want := "\x1b[97;1m标题\x1b[0m\n\x1b[90mx := 1\x1b[0m\n• 项\n▌ 引\n────\nraw\n"
	if colored != want {
		t.Errorf("彩色:\\n got %q\\nwant %q", colored, want)
	}
	plain := renderBlocks(t, term.Profile{TTY: false, Colors: term.LevelNone}, doc)
	wantPlain := "标题\nx := 1\n• 项\n▌ 引\n────\nraw\n"
	if plain != wantPlain {
		t.Errorf("无色:\\n got %q\\nwant %q", plain, wantPlain)
	}
}

func renderBlocks(t *testing.T, prof term.Profile, blocks []ir.Block) string {
	t.Helper()
	r := NewRenderer(prof)
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(r.Block(blk))
	}
	return b.String()
}

func TestRenderOrderedStart(t *testing.T) {
	got := NewRenderer(term.Profile{TTY: true, Colors: term.LevelNone}).Block(ir.List{Ordered: true, Start: 3, Items: []ir.ListItem{
		{Blocks: []ir.Block{ir.Paragraph{Inlines: []ir.Inline{ir.Span{Text: "a"}}}}},
	}})
	if got != "3. a\n" {
		t.Errorf("got %q", got)
	}
}
