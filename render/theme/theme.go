package theme

import rstyle "github.com/LaoQi/tanya/render/style"

// Semantics 是 UI 语义色集合，对应全局 Dim/Info/Warn/Ok/Error/Accent/Think/Run。
type Semantics struct {
	Dim, Info, Warn, Ok, Error, Accent, Think, Run rstyle.Style
}

// Scheme 是完整配色主题：语义色 + 提示符模板 + markdown 渲染样式。
type Scheme struct {
	Name   string
	Desc   string
	Sem    Semantics
	Prompt string
	MD     Theme
}

const DefaultPrompt = "[white]{cwd}[/] [blue]{model}[/] [yellow]{effort}[/] [green]{usage_summary}[/] [white]>[/] "

func fg(v uint8) rstyle.Style     { return rstyle.Style{Fg: rstyle.Color16(v)} }
func fgBold(v uint8) rstyle.Style { return rstyle.Style{Fg: rstyle.Color16(v), Attr: rstyle.AttrBold} }

func mdTheme(h1, h2, h3, h4, h5, h6, codeBlock, codeInline rstyle.Style) Theme {
	return Theme{
		Headings:    [6]rstyle.Style{h1, h2, h3, h4, h5, h6},
		CodeBlock:   codeBlock,
		CodeInline:  codeInline,
		QuotePrefix: "▌ ",
		Bullet:      "• ",
		Rule:        "────",
		TableHead:   fgBold(14),
		TableBorder: fg(8),
	}
}

var schemeList = []Scheme{
	{
		Name:   "default",
		Desc:   "默认（蓝绿黄高亮）",
		Prompt: DefaultPrompt,
		Sem: Semantics{
			Dim:    fg(8),
			Info:   fg(12),
			Warn:   fg(3),
			Ok:     fg(2),
			Error:  fg(9),
			Accent: rstyle.Style{Attr: rstyle.AttrReverse},
			Think:  fg(5),
			Run:    fg(6),
		},
		MD: DefaultTheme(),
	},
	{
		Name:   "minimal",
		Desc:   "克制灰阶，层级靠粗细",
		Prompt: "[white bold]{cwd}[/] [white]{model}[/] [bright_black]{effort}[/] [bright_green]{usage_summary}[/] [white]>[/] ",
		Sem: Semantics{
			Dim:    fg(8),
			Info:   fg(7),
			Warn:   fg(3),
			Ok:     fg(10),
			Error:  fg(9),
			Accent: rstyle.Style{Attr: rstyle.AttrReverse},
			Think:  fg(5),
			Run:    fg(6),
		},
		MD: mdTheme(fgBold(15), fgBold(7), fgBold(8), fg(8), fg(8), fg(8), fg(8), rstyle.Style{Attr: rstyle.AttrUnderline}),
	},
	{
		Name:   "solar",
		Desc:   "冷暖青金，低饱和护眼",
		Prompt: "[cyan]{cwd}[/] [white]{model}[/] [yellow]{effort}[/] [bright_green]{usage_summary}[/] [white]>[/] ",
		Sem: Semantics{
			Dim:    fg(8),
			Info:   fg(6),
			Warn:   fg(3),
			Ok:     fg(10),
			Error:  fg(9),
			Accent: rstyle.Style{Attr: rstyle.AttrReverse},
			Think:  fg(5),
			Run:    fg(6),
		},
		MD: mdTheme(fgBold(15), fgBold(6), fgBold(3), fg(8), fg(8), fg(8), fg(8), fg(6)),
	},
	{
		Name:   "vivid",
		Desc:   "高对比霓虹",
		Prompt: "[bright_blue]{cwd}[/] [bright_white]{model}[/] [bright_yellow]{effort}[/] [bright_green]{usage_summary}[/] [white]>[/] ",
		Sem: Semantics{
			Dim:    fg(8),
			Info:   fg(14),
			Warn:   fg(11),
			Ok:     fg(10),
			Error:  fg(9),
			Accent: rstyle.Style{Attr: rstyle.AttrReverse},
			Think:  fg(5),
			Run:    fg(6),
		},
		MD: mdTheme(fgBold(15), fgBold(13), fgBold(12), fg(7), fg(8), fg(8), fg(8), fg(10)),
	},
	{
		Name:   "nord",
		Desc:   "冷蓝灰（北极）",
		Prompt: "[bright_cyan]{cwd}[/] [white]{model}[/] [bright_blue]{effort}[/] [bright_green]{usage_summary}[/] [white]>[/] ",
		Sem: Semantics{
			Dim:    fg(8),
			Info:   fg(12),
			Warn:   fg(3),
			Ok:     fg(10),
			Error:  fg(9),
			Accent: rstyle.Style{Attr: rstyle.AttrReverse},
			Think:  fg(5),
			Run:    fg(6),
		},
		MD: mdTheme(fgBold(15), fgBold(14), fgBold(6), fg(8), fg(8), fg(8), fg(8), fg(12)),
	},
	{
		Name:   "gruv",
		Desc:   "暖金复古（Gruvbox 精神）",
		Prompt: "[bright_yellow]{cwd}[/] [white]{model}[/] [yellow]{effort}[/] [bright_green]{usage_summary}[/] [white]>[/] ",
		Sem: Semantics{
			Dim:    fg(8),
			Info:   fg(3),
			Warn:   fg(11),
			Ok:     fg(10),
			Error:  fg(9),
			Accent: rstyle.Style{Attr: rstyle.AttrReverse},
			Think:  fg(5),
			Run:    fg(6),
		},
		MD: mdTheme(fgBold(15), fgBold(3), fgBold(11), fg(8), fg(8), fg(8), fg(8), fg(10)),
	},
	{
		Name:   "dusk",
		Desc:   "蓝紫夜（Tokyo Night 精神）",
		Prompt: "[bright_magenta]{cwd}[/] [white]{model}[/] [bright_cyan]{effort}[/] [bright_green]{usage_summary}[/] [white]>[/] ",
		Sem: Semantics{
			Dim:    fg(8),
			Info:   fg(12),
			Warn:   fg(3),
			Ok:     fg(10),
			Error:  fg(9),
			Accent: rstyle.Style{Attr: rstyle.AttrReverse},
			Think:  fg(5),
			Run:    fg(6),
		},
		MD: mdTheme(fgBold(15), fgBold(12), fgBold(13), fg(8), fg(8), fg(8), fg(8), fg(14)),
	},
}

func Names() []string {
	out := make([]string, len(schemeList))
	for i, s := range schemeList {
		out[i] = s.Name
	}
	return out
}

func Has(name string) bool {
	_, ok := Lookup(name)
	return ok
}

func Lookup(name string) (Scheme, bool) {
	for _, s := range schemeList {
		if s.Name == name {
			return s, true
		}
	}
	return Scheme{}, false
}

type Theme struct {
	Headings    [6]rstyle.Style
	CodeBlock   rstyle.Style
	CodeInline  rstyle.Style
	QuotePrefix string
	Bullet      string
	Rule        string
	TableHead   rstyle.Style
	TableBorder rstyle.Style
}

func DefaultTheme() Theme {
	return Theme{
		Headings: [6]rstyle.Style{
			{Attr: rstyle.AttrBold, Fg: rstyle.Color16(15)},
			{Attr: rstyle.AttrBold, Fg: rstyle.Color16(14)},
			{Attr: rstyle.AttrBold, Fg: rstyle.Color16(12)},
			{Fg: rstyle.Color16(7)},
			{Fg: rstyle.Color16(8)},
			{Fg: rstyle.Color16(8)},
		},
		CodeBlock:   rstyle.Style{Fg: rstyle.Color16(8)},
		CodeInline:  rstyle.Style{Fg: rstyle.Color16(10)},
		QuotePrefix: "▌ ",
		Bullet:      "• ",
		Rule:        "────",
		TableHead:   fgBold(14),
		TableBorder: fg(8),
	}
}

func (t Theme) Heading(level int) rstyle.Style {
	if level < 1 || level > 6 {
		return rstyle.Style{}
	}
	return t.Headings[level-1]
}
