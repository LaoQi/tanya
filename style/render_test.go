package style

import "testing"

func TestRenderBlockProfiles(t *testing.T) {
	doc := Doc(
		Heading{Level: 1, Inlines: []Inline{Span{Text: "标题"}}},
		CodeBlock{Lang: "go", Lines: []string{"x := 1"}},
		List{Ordered: false, Items: []ListItem{{Blocks: []Block{P(Span{Text: "项"})}}}},
		Quote{Blocks: []Block{P(Span{Text: "引"})}},
		Rule{},
		RawText{Text: "raw"},
	)
	colored := NewRenderer(Profile{TTY: true, Colors: Level16}).Doc(doc)
	want := "\x1b[97;1m标题\x1b[0m\n\x1b[90mx := 1\x1b[0m\n• 项\n▌ 引\n────\nraw\n"
	if colored != want {
		t.Errorf("彩色:\\n got %q\\nwant %q", colored, want)
	}
	plain := NewRenderer(Profile{TTY: false, Colors: LevelNone}).Doc(doc)
	wantPlain := "标题\nx := 1\n• 项\n▌ 引\n────\nraw\n"
	if plain != wantPlain {
		t.Errorf("无色:\\n got %q\\nwant %q", plain, wantPlain)
	}
}

func TestRenderOrderedStart(t *testing.T) {
	got := NewRenderer(Profile{TTY: true, Colors: LevelNone}).Block(List{Ordered: true, Start: 3, Items: []ListItem{
		{Blocks: []Block{P(Span{Text: "a"})}},
	}})
	if got != "3. a\n" {
		t.Errorf("got %q", got)
	}
}
