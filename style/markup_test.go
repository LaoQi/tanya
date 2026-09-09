package style

import "testing"

func spans(in []Inline) []Span {
	var out []Span
	for _, i := range in {
		if s, ok := i.(Span); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestParseMarkup(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Span
	}{
		{"纯文本", "hello", []Span{{Text: "hello"}}},
		{"单色", "[red]abc[/]", []Span{{Style: Style{Fg: Color16(1)}, Text: "abc"}}},
		{"色后文本", "[red]a[/]b", []Span{
			{Style: Style{Fg: Color16(1)}, Text: "a"},
			{Text: "b"},
		}},
		{"组合属性", "[red bold]x[/]", []Span{{Style: Style{Fg: Color16(1), Attr: AttrBold}, Text: "x"}}},
		{"多属性乱序", "[underline yellow]x[/]", []Span{{Style: Style{Fg: Color16(3), Attr: AttrUnderline}, Text: "x"}}},
		{"嵌套", "[red]a[bold]b[/]c[/]", []Span{
			{Style: Style{Fg: Color16(1)}, Text: "a"},
			{Style: Style{Fg: Color16(1), Attr: AttrBold}, Text: "b"},
			{Style: Style{Fg: Color16(1)}, Text: "c"},
		}},
		{"语义名", "[dim]a[/] [info]b[/]", []Span{
			{Style: Style{Fg: Color16(8)}, Text: "a"},
			{Text: " "},
			{Style: Style{Fg: Color16(12)}, Text: "b"},
		}},
		{"spinner 语义名", "[warn]a[/] [think]b[/] [run]c[/]", []Span{
			{Style: Style{Fg: Color16(3)}, Text: "a"},
			{Text: " "},
			{Style: Style{Fg: Color16(5)}, Text: "b"},
			{Text: " "},
			{Style: Style{Fg: Color16(6)}, Text: "c"},
		}},
		{"未知名原样", "a[xyz]b", []Span{{Text: "a[xyz]b"}}},
		{"部分未知整标签原样", "[red xyz]a[/]", []Span{{Text: "[red xyz]a[/]"}}},
		{"未闭合着色到行尾", "[red]abc", []Span{{Style: Style{Fg: Color16(1)}, Text: "abc"}}},
		{"游离闭合原样", "a[/]b", []Span{{Text: "a[/]b"}}},
		{"空标签原样", "a[]b", []Span{{Text: "a[]b"}}},
		{"无右括号原样", "a[red", []Span{{Text: "a[red"}}},
		{"中文", "[green]你好[/]", []Span{{Style: Style{Fg: Color16(2)}, Text: "你好"}}},
		{"数组下标不误吞", "arr[0] = 1", []Span{{Text: "arr[0] = 1"}}},
	}
	for _, c := range cases {
		got := spans(ParseMarkup(c.in))
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
	got := spans(ParseMarkup("a[red][/b"))
	if len(got) != 2 || got[0].Text != "a" || got[1].Text != "[/b" || got[1].Style.Fg.V16 != 1 {
		t.Errorf("未闭合后字面量应延续样式: %+v", got)
	}
}
