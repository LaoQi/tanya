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

func TestMarkdownFenceWatchdogRecovers(t *testing.T) {
	input := "```\n" + strings.Repeat("code\n", fenceLineLimit+1) + "after\n"
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, input)
	if n := strings.Count(got, "code"); n != fenceLineLimit+1 {
		t.Errorf("超限围栏内容应原样出块: %d 行", n)
	}
	if !strings.HasSuffix(got, "after\n") {
		t.Errorf("围栏关闭后应恢复普通解析，后续行不得被吞: %q", got[max(0, len(got)-40):])
	}
	if rest := closeText(t, buf); rest != "" {
		t.Errorf("看门狗已降级出块，Close 不应再有残留: %q", rest)
	}
}

func TestMarkdownPendingWatchdogFlushes(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, strings.Repeat("a", pendingByteLimit+1))
	if got != strings.Repeat("a", pendingByteLimit+1)+"\n" {
		t.Errorf("超限无换行缓冲应提前出段: %d 字节", len(got))
	}
	if after := blocksText(t, buf, "tail\n") + closeText(t, buf); after != "tail\n" {
		t.Errorf("降级后应继续流式解析: %q", after)
	}
}

func TestMarkdownFenceLongLineStaysInFence(t *testing.T) {
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "```\n"+strings.Repeat("x", pendingByteLimit+1)+"\n```\n")
	if strings.Contains(got, "x\nx") {
		t.Errorf("围栏内超限行不得降级为段落: %q", got[:40])
	}
	want := "\x1b[90m" + strings.Repeat("x", pendingByteLimit+1) + "\x1b[0m\n"
	if got != want {
		t.Errorf("围栏内超限行应并入代码块: got %d 字节", len(got))
	}
}

func TestMarkdownFenceByteWatchdog(t *testing.T) {
	line := strings.Repeat("y", 64<<10)
	buf := NewMarkdownBuf()
	got := blocksText(t, buf, "```\n"+strings.Repeat(line+"\n", 5)+"after\n")
	if !strings.HasSuffix(got, "after\n") {
		t.Errorf("围栏字节超限应关闭围栏并恢复解析: %d 字节", len(got))
	}
	if n := strings.Count(got, line); n != 5 {
		t.Errorf("所有行都应保留: %d 行", n)
	}
	if !strings.Contains(got, "\x1b[0m\n"+line) {
		t.Errorf("超限后的行应回到普通段落渲染")
	}
}

func plainBlocks(t *testing.T, buf *MarkdownBuf, delta string) string {
	t.Helper()
	r := render.NewRenderer(term.Profile{TTY: true, Colors: term.LevelNone})
	var b strings.Builder
	for _, blk := range buf.Write(delta) {
		b.WriteString(r.Block(blk))
	}
	return b.String()
}

func plainClose(t *testing.T, buf *MarkdownBuf) string {
	t.Helper()
	r := render.NewRenderer(term.Profile{TTY: true, Colors: term.LevelNone})
	var b strings.Builder
	for _, blk := range buf.Close() {
		b.WriteString(r.Block(blk))
	}
	return b.String()
}

func TestMarkdownTableAlignAndBox(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| name | qty |\n|:-----|----:|\n| a | 1 |\n| bb | 22 |\n\n")
	got += plainClose(t, buf)
	want := "┌──────┬─────┐\n" +
		"│ name │ qty │\n" +
		"├──────┼─────┤\n" +
		"│ a    │   1 │\n" +
		"│ bb   │  22 │\n" +
		"└──────┴─────┘\n\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownTableStreamSplit(t *testing.T) {
	buf := NewMarkdownBuf()
	if got := plainBlocks(t, buf, "| a | b |\n"); got != "" {
		t.Errorf("表头未确认不应出块: %q", got)
	}
	if got := plainBlocks(t, buf, "|---|---|\n"); got != "" {
		t.Errorf("列宽未定时不应出块: %q", got)
	}
	got := plainBlocks(t, buf, "| 1 | 2 |\n")
	want := "┌───┬───┐\n│ a │ b │\n├───┼───┤\n│ 1 │ 2 │\n"
	if got != want {
		t.Errorf("首数据行应带出表头与上框:\n got %q\nwant %q", got, want)
	}
	got = plainBlocks(t, buf, "| 3 | 4 |\n") + plainClose(t, buf)
	want = "│ 3 │ 4 │\n└───┴───┘\n"
	if got != want {
		t.Errorf("续行与下框:\n got %q\nwant %q", got, want)
	}
}

func TestMarkdownTableCenterAlign(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| a | bb |\n|:-:|:-:|\n| 1 | 2 |\n")
	want := "┌───┬────┐\n│ a │ bb │\n├───┼────┤\n│ 1 │ 2  │\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownTableCandidateFallback(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "a | b\nplain\n") + plainClose(t, buf)
	if got != "a | b\nplain\n" {
		t.Errorf("非表格的竖线行应回退为段落: %q", got)
	}
}

func TestMarkdownTableShortRowPadsAndExtraDropped(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| a | b | c |\n|---|---|---|\n| 1 |\n| 1 | 2 | 3 | 4 |\n")
	want := "┌───┬───┬───┐\n│ a │ b │ c │\n├───┼───┼───┤\n│ 1 │   │   │\n│ 1 │ 2 │ 3 │\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownTableEscapedPipe(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| a \\| b | c |\n|---|---|\n| x | y |\n")
	want := "┌───────┬───┐\n│ a | b │ c │\n├───────┼───┤\n│ x     │ y │\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownTableOverwideNotTruncated(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| a | b |\n|---|---|\n| 1 | 2 |\n| abcdefghij | y |\n")
	want := "┌───┬───┐\n│ a │ b │\n├───┼───┤\n│ 1 │ 2 │\n│ abcdefghij │ y │\n"
	if got != want {
		t.Errorf("超宽单元格应原样渲染:\n got %q\nwant %q", got, want)
	}
}

func TestMarkdownTableInlineWidth(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| **bold** | x |\n|---|---|\n| 1 | 2 |\n")
	want := "┌──────┬───┐\n│ bold │ x │\n├──────┼───┤\n│ 1    │ 2 │\n"
	if got != want {
		t.Errorf("列宽应按可见宽度计:\n got %q\nwant %q", got, want)
	}
}

func TestMarkdownTableCompactOnNarrowTerminal(t *testing.T) {
	buf := NewMarkdownBuf()
	buf.SetWidth(6)
	got := plainBlocks(t, buf, "| a | b |\n|---|---|\n| 1 | 2 |\n")
	want := "┌─┬─┐\n│a│b│\n├─┼─┤\n│1│2│\n"
	if got != want {
		t.Errorf("窄终端应切紧边距:\n got %q\nwant %q", got, want)
	}
	buf = NewMarkdownBuf()
	buf.SetWidth(200)
	got = plainBlocks(t, buf, "| a | b |\n|---|---|\n| 1 | 2 |\n")
	if !strings.HasPrefix(got, "┌───┬───┐\n") {
		t.Errorf("宽终端应保留松边距: %q", got)
	}
}

func TestMarkdownTableHeaderOnly(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| a | b |\n|---|---|\n") + plainClose(t, buf)
	want := "┌───┬───┐\n│ a │ b │\n├───┼───┤\n└───┴───┘\n"
	if got != want {
		t.Errorf("只有表头的表:\n got %q\nwant %q", got, want)
	}
}

func TestMarkdownTableEndsAndResumes(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| a | b |\n|---|---|\n| 1 | 2 |\nafter\n")
	want := "┌───┬───┐\n│ a │ b │\n├───┼───┤\n│ 1 │ 2 │\n└───┴───┘\nafter\n"
	if got != want {
		t.Errorf("表格结束应补下框并恢复普通解析:\n got %q\nwant %q", got, want)
	}
}

func TestMarkdownTableSeparatorMismatchFallsBack(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| a | b |\n|---|\n| 1 | 2 |\n") + plainClose(t, buf)
	want := "| a | b |\n|---|\n| 1 | 2 |\n"
	if got != want {
		t.Errorf("列数不匹配不应进表格: %q", got)
	}
}

func TestMarkdownTableQuoteNotTable(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "> a | b\n")
	if strings.Contains(got, "┌") {
		t.Errorf("引用行不应被当作表格: %q", got)
	}
}

func TestMarkdownTablePendingFlushCloses(t *testing.T) {
	buf := NewMarkdownBuf()
	if got := plainBlocks(t, buf, "| a | b |\n|---|---|\n"); got != "" {
		t.Errorf("确认后无数据行不应出块: %q", got)
	}
	long := strings.Repeat("x", pendingByteLimit+1)
	got := plainBlocks(t, buf, long)
	want := "┌───┬───┐\n│ a │ b │\n├───┼───┤\n└───┴───┘\n" + long + "\n"
	if got != want {
		t.Errorf("超长无换行应结算表格并降级段落:\n got %q…\nwant %q…", got[:60], want[:60])
	}
	if got := plainBlocks(t, buf, "tail\n") + plainClose(t, buf); got != "tail\n" {
		t.Errorf("降级后应恢复流式解析: %q", got)
	}
}

func TestMarkdownTableClosePendingRow(t *testing.T) {
	buf := NewMarkdownBuf()
	got := plainBlocks(t, buf, "| a | b |\n|---|---|\n| 1 | 2 |\ntail")
	want := "┌───┬───┐\n│ a │ b │\n├───┼───┤\n│ 1 │ 2 │\n"
	if got != want {
		t.Errorf("残行未换行不应出块: %q", got)
	}
	got = plainClose(t, buf)
	want = "└───┴───┘\ntail\n"
	if got != want {
		t.Errorf("Close 应先补下框再出残行:\n got %q\nwant %q", got, want)
	}
}
