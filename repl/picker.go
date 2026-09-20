package repl

import (
	"fmt"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/render/theme"
	"io"
	"os"
	"strings"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
)

// sessSummary 取候选摘要：摘要源自会话首条 user 消息（用户可粘贴任意内容），
// 必须折成单行并清洗控制序列 —— picker 与补全菜单都按"每项一行"定位。
func sessSummary(s agent.SessionInfo) string {
	if s.Archived {
		return term.OneLine(SessArchMark + s.Summary)
	}
	return term.OneLine(s.Summary)
}

// pickerTailReserve 是窗口末尾预留的空行：末项以 \r\n 收尾，块若占满整屏会触发滚屏，
// 使"上移 p.lines 行回到块首"的记账失效（表现为整块从屏幕顶部重画）。
const pickerTailReserve = 1

type sessionPicker struct {
	items  []agent.SessionInfo
	cursor int
	start  int // 窗口首项索引
	lines  int // 上次重绘占用行数（含标题），即下一次上移的行数
	size   readline.Size
	done   bool
	cancel bool
	sem    theme.Semantics
}

// visible 报告窗口容纳的项数：标题占一行并预留尾部余量；
// 终端尺寸不可知（Size 失败）时退回全量渲染，与窗口化之前语义一致。
func (p *sessionPicker) visible() int {
	if p.size.Rows <= 0 {
		return len(p.items)
	}
	w := p.size.Rows - 1 - pickerTailReserve
	if w < 1 {
		w = 1
	}
	if w > len(p.items) {
		w = len(p.items)
	}
	return w
}

// follow 平移窗口使光标可见：只在光标顶出窗口边界时滚动，避免每帧重新居中导致画面跳动。
func (p *sessionPicker) follow(win int) {
	if p.cursor < p.start {
		p.start = p.cursor
	}
	if p.cursor >= p.start+win {
		p.start = p.cursor - win + 1
	}
	if max := len(p.items) - win; p.start > max {
		p.start = max
	}
	if p.start < 0 {
		p.start = 0
	}
}

func (p *sessionPicker) handle(ev readline.KeyEvent) {
	switch ev.Code {
	case readline.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
	case readline.KeyDown:
		if p.cursor < len(p.items)-1 {
			p.cursor++
		}
	case readline.KeyEnter:
		p.done = true
	case readline.KeyEsc, readline.KeyCtrlC, readline.KeyCtrlD:
		p.done = true
		p.cancel = true
	case readline.KeyRune:
		if ev.Rune == 'q' {
			p.done = true
			p.cancel = true
		}
	}
}

// render 重绘整块：上移量按上次实际写入的行数记账（p.lines），且窗口行数不超过屏幕，
// 因此相对上移永不超出光标所在行（否则会被视口夹到顶行、整块从屏幕顶部重画）。
// 整块一次写出：避免逐行分片，并让 raw 期自绘保持原子。
func (p *sessionPicker) render(out io.Writer) {
	win := p.visible()
	if win <= 0 {
		return
	}
	if p.lines > 0 {
		fmt.Fprint(out, term.CursorUp(p.lines))
	}
	p.follow(win)
	var b strings.Builder
	title := fmt.Sprintf(PickTitle, p.cursor+1, len(p.items))
	if p.size.Cols > 0 {
		title = term.Truncate(title, p.size.Cols-1)
	}
	b.WriteString(term.ClearLineHome() + title + "\r\n")
	for i := p.start; i < p.start+win; i++ {
		s := p.items[i]
		mark := MsgMarkPlain
		if i == p.cursor {
			mark = p.sem.Ok.Sprint("> ")
		}
		line := fmt.Sprintf(SessRow, mark, s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, sessSummary(s))
		if p.size.Cols > 0 {
			line = term.Truncate(line, p.size.Cols-1)
		}
		b.WriteString(term.ClearLineHome() + line + term.ClearLine() + "\r\n")
	}
	p.lines = win + 1
	fmt.Fprint(out, b.String())
}

func pickSession(dev readline.Terminal, list []agent.SessionInfo, out io.Writer, sem theme.Semantics) (int, bool) {
	if len(list) == 0 {
		return -1, false
	}
	if err := dev.Raw(); err != nil {
		return -1, false
	}
	defer dev.Restore()
	p := &sessionPicker{items: list, sem: sem}
	if size, ok := dev.Size(); ok {
		p.size = size
	}
	p.render(out)
	for {
		ev, err := dev.ReadKey()
		if err != nil {
			return -1, false
		}
		p.handle(ev)
		if p.done {
			break
		}
		if size, ok := dev.Size(); ok && size != p.size {
			p.size = size
			p.lines = 0 // 尺寸变化：按旧行数上移会错位，放弃锚点另起一块
		}
		p.render(out)
	}
	if p.cancel {
		return -1, false
	}
	return p.cursor, true
}

func pickByNumber(list []agent.SessionInfo, out *output) (int, bool) {
	var b strings.Builder
	b.WriteString(PickNumTitle)
	for i, s := range list {
		fmt.Fprintf(&b, "  %-3d "+SessRow+"\n", i+1, "", s.ID, s.ModTime.Format("01-02 15:04"), s.Msgs, sessSummary(s))
	}
	b.WriteString(PickNumPrompt)
	out.emit(KindNotice, b.String())
	var n int
	if _, err := fmt.Fscan(os.Stdin, &n); err != nil || n < 1 || n > len(list) {
		return -1, false
	}
	return n - 1, true
}
