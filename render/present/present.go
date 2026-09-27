package present

import (
	"fmt"
	"strings"
	"time"

	"github.com/LaoQi/tanya/render/term"
)

const (
	MaxLines  = 8
	HeadLines = 6
	TailLines = 1
	TabWidth  = 4
)

// ArgsView 是工具参数视图：Inline 为可与工具名同行的单行候选（空串表示不可内联），
// Body 为块形态的正文行（已带前缀）。
type ArgsView struct {
	Inline string
	Body   []string
}

// View 是工具结果视图：Body 为已成品正文（含换行），Status 为状态行文案。
type View struct {
	Body   string
	Status string
}

// ToolView 是一个工具的自带渲染：两个回调都返回 ok=false 表示「本视图不处理，走通用回落」。
type ToolView struct {
	Args   func(argsJSON string, width int) (ArgsView, bool)
	Result func(text string, meta any, width, maxLines int) (View, bool)
}

// Notification 是通知载荷的成品形态：已清洗为单行、已截断，由触发方（repl）生成、
// 行为方（终端提示音/OSC/外部程序）只读。
type Notification struct {
	Title   string
	Content string
	Kind    string
}

// Registry 按工具名索引视图，装配期注入、运行期冻结（与工具注入同语义）。
type Registry struct {
	views map[string]ToolView
}

func NewRegistry() *Registry { return &Registry{views: make(map[string]ToolView)} }

func (r *Registry) Register(name string, v ToolView) {
	if r == nil || name == "" {
		return
	}
	r.views[name] = v
}

func (r *Registry) Args(name, argsJSON string, width int) (ArgsView, bool) {
	v, ok := r.lookup(name)
	if !ok || v.Args == nil {
		return ArgsView{}, false
	}
	return v.Args(argsJSON, width)
}

func (r *Registry) Result(name, text string, meta any, width, maxLines int) (View, bool) {
	v, ok := r.lookup(name)
	if !ok || v.Result == nil {
		return View{}, false
	}
	return v.Result(text, meta, width, maxLines)
}

func (r *Registry) lookup(name string) (ToolView, bool) {
	if r == nil {
		return ToolView{}, false
	}
	v, ok := r.views[name]
	return v, ok
}

// CapLines 行数超上限时保留头 6 行 + 省略行 + 尾 1 行；省略文案由调用方给出。
// width 是不含前缀的可用正文宽（省略行同样在此宽度内截断，加前缀后不越终端）。
func CapLines(lines []string, width int, omitFmt string) []string {
	if len(lines) <= MaxLines {
		return lines
	}
	omitted := len(lines) - HeadLines - TailLines
	out := make([]string, 0, MaxLines)
	out = append(out, lines[:HeadLines]...)
	out = append(out, term.Truncate(fmt.Sprintf(omitFmt, omitted), width))
	return append(out, lines[len(lines)-TailLines:]...)
}

func Prefixed(lines []string, prefix string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = prefix + l
	}
	return out
}

// PlainArgsView 原样展示参数文本：单行可内联，否则按前缀折行（坏 JSON 的兜底通道）。
func PlainArgsView(text, prefix, omitFmt string, width int) ArgsView {
	if text == "" {
		return ArgsView{}
	}
	v := ArgsView{Body: Prefixed(CapLines(term.Wrap(text, width-len(prefix)), width-len(prefix), omitFmt), prefix)}
	if !strings.Contains(text, "\n") {
		v.Inline = text
	}
	return v
}

// Indent 把正文行按块缩进（两个空格）拼成成品文本。
func Indent(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("  " + l + "\n")
	}
	return b.String()
}

// ExpandTabs 展开制表符：宽度表把 \t 当单列，与终端制表位不符，折行前必须先摊平，否则折行位置与显示不符。
func ExpandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	return strings.ReplaceAll(s, "\t", strings.Repeat(" ", TabWidth))
}

// TrimBlankEdges 去掉首尾空行但保留行首缩进——heredoc/多行脚本的缩进是命令结构的一部分。
func TrimBlankEdges(s string) string { return strings.Trim(s, "\n\r") }

// Duration 是耗时展示的单一实现（亚秒毫秒、其余一位小数的秒）。
func Duration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}
