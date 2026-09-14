package render

import (
	"github.com/LaoQi/tanyan/render/ir"
	"testing"

	"github.com/LaoQi/tanyan/render/term"
)

func TestParseTemplateMarkup(t *testing.T) {
	tpl, err := ParseTemplate("[white]{cwd}[/] [blue]{model}[/]", defSem())
	if err != nil {
		t.Fatal(err)
	}
	if tpl.passthrough {
		t.Error("无 ANSI 不应进入 passthrough")
	}
	resolve := func(name string) (string, bool) {
		if name == "cwd" {
			return "/home/u", true
		}
		if name == "model" {
			return "deepseek", true
		}
		return "", false
	}
	got := tpl.Render(resolve)
	want := "\x1b[37m/home/u\x1b[0m \x1b[34mdeepseek\x1b[0m"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTemplateDefaultPromptEquivalence(t *testing.T) {
	src := "[white]{cwd}[/] [blue]{model}[/] [yellow]{effort}[/] [green]{stat}[/] [white]>[/] "
	tpl, err := ParseTemplate(src, defSem())
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(name string) (string, bool) {
		switch name {
		case "cwd":
			return "~/p", true
		case "model":
			return "m1", true
		case "effort":
			return "low", true
		case "stat":
			return "ok", true
		}
		return "", false
	}
	got := tpl.Render(resolve)
	want := "\x1b[37m~/p\x1b[0m \x1b[34mm1\x1b[0m \x1b[33mlow\x1b[0m \x1b[32mok\x1b[0m \x1b[37m>\x1b[0m "
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTemplateUnknownPlaceholderKept(t *testing.T) {
	tpl, _ := ParseTemplate("[white]{cwd} {nope}[/]", defSem())
	resolve := func(name string) (string, bool) {
		if name == "cwd" {
			return "/x", true
		}
		return "", false
	}
	got := tpl.Render(resolve)
	want := "\x1b[37m/x {nope}\x1b[0m"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTemplateResolveFalseKeepsLiteral(t *testing.T) {
	tpl, _ := ParseTemplate("[red]x{nope}[/]", defSem())
	resolve := func(string) (string, bool) { return "", false }
	got := tpl.Render(resolve)
	want := "\x1b[31mx{nope}\x1b[0m"
	if got != want {
		t.Errorf("resolve false 应保留字面量: %q", got)
	}
}

func TestTemplateValueWithMarkupChars(t *testing.T) {
	tpl, _ := ParseTemplate("[white]{cwd}[/]", defSem())
	resolve := func(name string) (string, bool) {
		return "~[red]{x}~", true
	}
	got := tpl.Render(resolve)
	want := "\x1b[37m~[red]{x}~\x1b[0m"
	if got != want {
		t.Errorf("值不应被二次解析: %q", got)
	}
}

func TestTemplateEmptyValueSpanSkipped(t *testing.T) {
	tpl, _ := ParseTemplate("[yellow]{effort}[/] [white]>[/] ", defSem())
	resolve := func(name string) (string, bool) {
		if name == "effort" {
			return "", true
		}
		if name == "stat" {
			return "", true
		}
		return ">", name == "cwd"
	}
	got := tpl.Render(resolve)
	want := " \x1b[37m>\x1b[0m "
	if got != want {
		t.Errorf("空值应跳过 span: %q", got)
	}
}

func TestTemplatePassthroughANSI(t *testing.T) {
	tpl, err := ParseTemplate("\x1b[37m{cwd}\x1b[0m >", defSem())
	if err != nil {
		t.Fatal(err)
	}
	if !tpl.passthrough {
		t.Fatal("含 ANSI 应进入 passthrough")
	}
	resolve := func(name string) (string, bool) {
		return "/p", name == "cwd"
	}
	if got := tpl.Render(resolve); got != "\x1b[37m/p\x1b[0m >" {
		t.Errorf("彩色直通: %q", got)
	}
	defer term.SetProfile(term.GetProfile())
	term.SetProfile(term.Profile{TTY: false, Colors: term.LevelNone})
	if got := tpl.Render(resolve); got != "/p >" {
		t.Errorf("无色应剥离: %q", got)
	}
}

func TestBindSplitsSpans(t *testing.T) {
	tpl, _ := ParseTemplate("[red]a {cwd} b[/]", defSem())
	resolve := func(name string) (string, bool) {
		return "X", name == "cwd"
	}
	got := spans(tpl.Bind(resolve))
	if len(got) != 3 || got[0].Text != "a " || got[1].Text != "X" || got[2].Text != " b" {
		t.Errorf("占位符应切分 span: %+v", got)
	}
	for _, sp := range got {
		if sp.Style.Fg.V16 != 1 {
			t.Errorf("切分后样式丢失: %+v", sp)
		}
	}
}

func spans(in []ir.Inline) []ir.Span {
	var out []ir.Span
	for _, i := range in {
		if s, ok := i.(ir.Span); ok {
			out = append(out, s)
		}
	}
	return out
}
