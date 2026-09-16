package markdown

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanya/render"
	"github.com/LaoQi/tanya/render/ir"
	rstyle "github.com/LaoQi/tanya/render/style"
	"github.com/LaoQi/tanya/render/term"
)

func blocksText(t *testing.T, buf *MarkdownBuf, delta string) string {
	t.Helper()
	var b strings.Builder
	for _, blk := range buf.Write(delta) {
		b.WriteString(render.NewRenderer(term.GetProfile()).Block(blk))
	}
	return b.String()
}

func closeText(t *testing.T, buf *MarkdownBuf) string {
	t.Helper()
	var b strings.Builder
	for _, blk := range buf.Close() {
		b.WriteString(render.NewRenderer(term.GetProfile()).Block(blk))
	}
	return b.String()
}

func TestMarkdownStreamParagraphs(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "Hello **wor")
	if got != "" {
		t.Errorf("未换行不应出块: %q", got)
	}
	got += blocksText(t, buf, "ld!**\n\nBye\n")
	got += closeText(t, buf)
	want := "Hello \x1b[1mworld!\x1b[0m\n\nBye\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownBlankLineParagraph(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "a\n\nb\n") + closeText(t, buf)
	if got != "a\n\nb\n" {
		t.Errorf("got %q", got)
	}
}

func TestMarkdownFence(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "```go\nfmt.Println(1)\nfmt.Println(2)\n```\nafter\n") + closeText(t, buf)
	want := "\x1b[90mfmt.Println(1)\x1b[0m\n\x1b[90mfmt.Println(2)\x1b[0m\nafter\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownFenceUnclosedCloseRaw(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "text\n```py\nx = 1\n") + closeText(t, buf)
	want := "text\n```py\nx = 1\n"
	if got != want {
		t.Errorf("未闭合围栏应原样: %q", got)
	}
}

func TestMarkdownFlushKeepsFenceOpen(t *testing.T) {
	buf := NewMarkdownBuf()
	if got := blocksText(t, buf, "```go\nfmt."); got != "" {
		t.Errorf("Flush 期间围栏内容不应出块: %q", got)
	}
	got := blocksText(t, buf, "Println()\n```\n") + closeText(t, buf)
	want := "\x1b[90mfmt.Println()\x1b[0m\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownList(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "- a\n- b\nx\n") + closeText(t, buf)
	want := "• a\n• b\nx\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownOrderedList(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "1. a\n2. b\n\n") + closeText(t, buf)
	want := "1. a\n2. b\n\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownQuote(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "> q1\n>q2\n\nafter\n") + closeText(t, buf)
	want := "▌ q1\n▌ q2\n\nafter\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownHeadingAndRule(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "# T1\n## T2\n---\nbody\n") + closeText(t, buf)
	want := "\x1b[97;1mT1\x1b[0m\n\x1b[96;1mT2\x1b[0m\n────\nbody\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownControlStripped(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "a\x1b[31mb\x07c\rd\n") + closeText(t, buf)
	if got != "abcd\n" {
		t.Errorf("控制字符应剥离: %q", got)
	}
}

func TestMarkdownHTMLLiteral(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "<b>x</b>\n") + closeText(t, buf)
	if got != "<b>x</b>\n" {
		t.Errorf("HTML 应原样: %q", got)
	}
}

func TestMarkdownClosePartialLine(t *testing.T) {
	buf := NewMarkdownBuf()
	blocksText(t, buf, "abc")
	got := closeText(t, buf)
	if got != "abc\n" {
		t.Errorf("got %q", got)
	}
}

func TestMarkdownHashTagNotHeading(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "#tag 热点\n") + closeText(t, buf)
	if got != "#tag 热点\n" {
		t.Errorf("got %q", got)
	}
}

func TestParseInline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"粗斜代码混排", "**b** *i* `c`", "\x1b[1mb\x1b[0m \x1b[3mi\x1b[0m \x1b[92mc\x1b[0m"},
		{"未闭合粗体原样", "**abc", "**abc"},
		{"星号列表符不误判", "* a *b*", "* a \x1b[3mb\x1b[0m"},
		{"下划线不解析", "a_1 b_2", "a_1 b_2"},
		{"代码内字面量", "`**x**`", "\x1b[92m**x**\x1b[0m"},
		{"未闭合代码原样", "`abc", "`abc"},
		{"纯文本", "plain 中文", "plain 中文"},
		{"粗体含中文", "**你好**世界", "\x1b[1m你好\x1b[0m世界"},
	}
	for _, c := range cases {
		got := render.Sprint(ParseInline(c.in)...)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseInlineSpanStructure(t *testing.T) {
	got := ParseInline("**b** tail")
	if len(got) != 2 {
		t.Fatalf("spans = %d", len(got))
	}
	b, ok := got[0].(ir.Span)
	if !ok || b.Style != (rstyle.Style{Attr: rstyle.AttrBold}) || b.Text != "b" {
		t.Errorf("span[0] = %+v", got[0])
	}
	if tail, ok := got[1].(ir.Span); !ok || tail.Text != " tail" {
		t.Errorf("span[1] = %+v", got[1])
	}
}

func TestInlineMergeAcrossCodeSpan(t *testing.T) {
	got := render.Sprint(
		ir.Span{Style: rstyle.Style{Attr: rstyle.AttrBold}, Text: "a"},
		ir.CodeSpan{Text: "c"},
		ir.Span{Style: rstyle.Style{Attr: rstyle.AttrBold}, Text: "b"},
	)
	want := "\x1b[1ma\x1b[0m\x1b[92mc\x1b[0m\x1b[1mb\x1b[0m"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownClosePendingStripsANSI(t *testing.T) {
	buf := NewMarkdownBuf()
	blocksText(t, buf, "a\x1b[31mb")
	got := closeText(t, buf)
	if got != "ab\n" {
		t.Errorf("残行应剥离转义: %q", got)
	}
}

func TestInlineCodeBrightAndDistinct(t *testing.T) {
	inline := render.Sprint(ir.CodeSpan{Text: "x"})
	if strings.Contains(inline, "\x1b[90m") {
		t.Errorf("行内 code 不应使用暗色: %q", inline)
	}
	block := render.NewRenderer(term.GetProfile()).Block(ir.CodeBlock{Lines: []string{"x"}})
	if inline == block {
		t.Errorf("行内 code 与代码块应有不同配色: %q", inline)
	}
}
