package render

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanya/render/ir"
	rstyle "github.com/LaoQi/tanya/render/style"
	"github.com/LaoQi/tanya/render/term"
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

func span(t string) []ir.Inline { return []ir.Inline{ir.Span{Text: t}} }

func tableFixture() ir.Table {
	return ir.Table{
		Aligns: []ir.Align{ir.AlignLeft, ir.AlignRight},
		Widths: []int{4, 3},
		Rows: []ir.TableRow{
			{Cells: [][]ir.Inline{span("name"), span("qty")}, Header: true},
			{Cells: [][]ir.Inline{span("a"), span("1")}},
		},
		Top:    true,
		Bottom: true,
	}
}

func TestRenderTableBox(t *testing.T) {
	got := renderBlocks(t, term.Profile{TTY: true, Colors: term.LevelNone}, []ir.Block{tableFixture()})
	want := "┌──────┬─────┐\n│ name │ qty │\n├──────┼─────┤\n│ a    │   1 │\n└──────┴─────┘\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTableCompact(t *testing.T) {
	tab := tableFixture()
	tab.Compact = true
	got := renderBlocks(t, term.Profile{TTY: true, Colors: term.LevelNone}, []ir.Block{tab})
	want := "┌────┬───┐\n│name│qty│\n├────┼───┤\n│a   │  1│\n└────┴───┘\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTableStreamedContinuation(t *testing.T) {
	body := ir.Table{
		Aligns: []ir.Align{ir.AlignLeft},
		Widths: []int{3},
		Rows:   []ir.TableRow{{Cells: [][]ir.Inline{span("xyz")}}},
	}
	got := renderBlocks(t, term.Profile{TTY: true, Colors: term.LevelNone}, []ir.Block{body})
	if got != "│ xyz │\n" {
		t.Errorf("续行块只出行本身: %q", got)
	}
	end := ir.Table{Aligns: []ir.Align{ir.AlignLeft}, Widths: []int{3}, Bottom: true}
	got = renderBlocks(t, term.Profile{TTY: true, Colors: term.LevelNone}, []ir.Block{end})
	if got != "└─────┘\n" {
		t.Errorf("收尾块只出下框: %q", got)
	}
}

func TestRenderTableOverwideCellUnpadded(t *testing.T) {
	tab := ir.Table{
		Aligns: []ir.Align{ir.AlignLeft},
		Widths: []int{3},
		Rows:   []ir.TableRow{{Cells: [][]ir.Inline{span("abcdef")}}},
	}
	got := renderBlocks(t, term.Profile{TTY: true, Colors: term.LevelNone}, []ir.Block{tab})
	if got != "│ abcdef │\n" {
		t.Errorf("超宽单元格应原样: %q", got)
	}
}

func TestRenderTableStyled(t *testing.T) {
	got := renderBlocks(t, term.Profile{TTY: true, Colors: term.Level16}, []ir.Block{tableFixture()})
	for _, want := range []string{"\x1b[90m┌", "\x1b[96;1mname\x1b[0m", "\x1b[90m│\x1b[0m"} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q in %q", want, got)
		}
	}
	if !strings.Contains(got, "\x1b[96;1mname\x1b[0m \x1b[90m│") {
		t.Errorf("表头样式不应吞掉填充空格: %q", got)
	}
}

func TestRenderTableHeaderKeepsInlineStyle(t *testing.T) {
	tab := ir.Table{
		Aligns: []ir.Align{ir.AlignLeft},
		Widths: []int{4},
		Rows: []ir.TableRow{{Cells: [][]ir.Inline{{
			ir.Span{Style: rstyle.Style{Attr: rstyle.AttrItalic}, Text: "it"},
		}}, Header: true}},
	}
	got := renderBlocks(t, term.Profile{TTY: true, Colors: term.Level16}, []ir.Block{tab})
	if !strings.Contains(got, "\x1b[96;1;3mit\x1b[0m") {
		t.Errorf("单元格自带样式应与表头样式合并: %q", got)
	}
}
