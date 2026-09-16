package markup

import (
	"github.com/LaoQi/tanya/render/ir"
	"github.com/LaoQi/tanya/render/theme"
	"testing"

	rstyle "github.com/LaoQi/tanya/render/style"
)

func testSemantics() theme.Semantics {
	s, _ := theme.Lookup("default")
	return s.Sem
}

var testSem = testSemantics()

func spans(in []ir.Inline) []ir.Span {
	var out []ir.Span
	for _, i := range in {
		if s, ok := i.(ir.Span); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestParseMarkup(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []ir.Span
	}{
		{"纯文本", "hello", []ir.Span{{Text: "hello"}}},
		{"单色", "[red]abc[/]", []ir.Span{{Style: rstyle.Style{Fg: rstyle.Color16(1)}, Text: "abc"}}},
		{"色后文本", "[red]a[/]b", []ir.Span{
			{Style: rstyle.Style{Fg: rstyle.Color16(1)}, Text: "a"},
			{Text: "b"},
		}},
		{"组合属性", "[red bold]x[/]", []ir.Span{{Style: rstyle.Style{Fg: rstyle.Color16(1), Attr: rstyle.AttrBold}, Text: "x"}}},
		{"多属性乱序", "[underline yellow]x[/]", []ir.Span{{Style: rstyle.Style{Fg: rstyle.Color16(3), Attr: rstyle.AttrUnderline}, Text: "x"}}},
		{"嵌套", "[red]a[bold]b[/]c[/]", []ir.Span{
			{Style: rstyle.Style{Fg: rstyle.Color16(1)}, Text: "a"},
			{Style: rstyle.Style{Fg: rstyle.Color16(1), Attr: rstyle.AttrBold}, Text: "b"},
			{Style: rstyle.Style{Fg: rstyle.Color16(1)}, Text: "c"},
		}},
		{"语义名", "[dim]a[/] [info]b[/]", []ir.Span{
			{Style: rstyle.Style{Fg: rstyle.Color16(8)}, Text: "a"},
			{Text: " "},
			{Style: rstyle.Style{Fg: rstyle.Color16(12)}, Text: "b"},
		}},
		{"spinner 语义名", "[warn]a[/] [think]b[/] [run]c[/]", []ir.Span{
			{Style: rstyle.Style{Fg: rstyle.Color16(3)}, Text: "a"},
			{Text: " "},
			{Style: rstyle.Style{Fg: rstyle.Color16(5)}, Text: "b"},
			{Text: " "},
			{Style: rstyle.Style{Fg: rstyle.Color16(6)}, Text: "c"},
		}},
		{"未知名原样", "a[xyz]b", []ir.Span{{Text: "a[xyz]b"}}},
		{"部分未知整标签原样", "[red xyz]a[/]", []ir.Span{{Text: "[red xyz]a[/]"}}},
		{"未闭合着色到行尾", "[red]abc", []ir.Span{{Style: rstyle.Style{Fg: rstyle.Color16(1)}, Text: "abc"}}},
		{"游离闭合原样", "a[/]b", []ir.Span{{Text: "a[/]b"}}},
		{"空标签原样", "a[]b", []ir.Span{{Text: "a[]b"}}},
		{"无右括号原样", "a[red", []ir.Span{{Text: "a[red"}}},
		{"中文", "[green]你好[/]", []ir.Span{{Style: rstyle.Style{Fg: rstyle.Color16(2)}, Text: "你好"}}},
		{"数组下标不误吞", "arr[0] = 1", []ir.Span{{Text: "arr[0] = 1"}}},
	}
	for _, c := range cases {
		got := spans(ParseMarkup(c.in, testSem))
		if len(got) != len(c.want) {
			t.Errorf("%s: got %d spans %+v, want %d", c.name, len(got), got, len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: span[%d] = %+v, want %+v", c.name, i, got[i], c.want[i])
			}
		}
	}
}

func TestParseMarkupUnclosedThenLiteral(t *testing.T) {
	got := spans(ParseMarkup("a[red][/b", testSem))
	if len(got) != 2 || got[0].Text != "a" || got[1].Text != "[/b" || got[1].Style.Fg.V16 != 1 {
		t.Errorf("未闭合后字面量应延续样式: %+v", got)
	}
}
