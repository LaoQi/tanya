package repl

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/render/term"
)

func testSessions(n int) []agent.SessionInfo {
	var list []agent.SessionInfo
	for i := 0; i < n; i++ {
		list = append(list, agent.SessionInfo{
			ID:      fmt.Sprintf("20260901-%06d", i),
			Summary: fmt.Sprintf("会话 %d", i),
			Msgs:    2,
		})
	}
	return list
}

func TestPickerCursorBounds(t *testing.T) {
	p := &sessionPicker{items: testSessions(3)}
	p.handle(readline.KeyEvent{Code: readline.KeyUp})
	if p.cursor != 0 {
		t.Error("顶部 Up 不应越界")
	}
	p.handle(readline.KeyEvent{Code: readline.KeyDown})
	p.handle(readline.KeyEvent{Code: readline.KeyDown})
	p.handle(readline.KeyEvent{Code: readline.KeyDown})
	if p.cursor != 2 {
		t.Error("底部 Down 不应越界")
	}
}

func TestPickerConfirmAndCancel(t *testing.T) {
	p := &sessionPicker{items: testSessions(3), cursor: 1}
	p.handle(readline.KeyEvent{Code: readline.KeyEnter})
	if !p.done || p.cancel {
		t.Error("Enter 应确认")
	}
	p2 := &sessionPicker{items: testSessions(3)}
	p2.handle(readline.KeyEvent{Code: readline.KeyCtrlD})
	if !p2.done || !p2.cancel {
		t.Error("Ctrl+D 应取消")
	}
	p3 := &sessionPicker{items: testSessions(3)}
	p3.handle(readline.KeyEvent{Code: readline.KeyRune, Rune: 'q'})
	if !p3.done || !p3.cancel {
		t.Error("q 应取消")
	}
}

func TestPickerRender(t *testing.T) {
	p := &sessionPicker{items: testSessions(2), cursor: 1, sem: testSem(), size: readline.Size{Cols: 80, Rows: 24}}
	var buf bytes.Buffer
	p.render(&buf)
	s := buf.String()
	if !strings.Contains(s, "选择会话（2/2，") {
		t.Errorf("标题应带位置指示: %q", s)
	}
	if !bytes.Contains(buf.Bytes(), []byte("\x1b[32m> \x1b[0m20260901-000001")) {
		t.Errorf("当前项应有高亮标记: %q", s)
	}
	var buf2 bytes.Buffer
	p.render(&buf2)
	if !bytes.Contains(buf2.Bytes(), []byte("\x1b[3A")) {
		t.Errorf("重绘应上移光标: %q", buf2.String())
	}
}

func TestPickerUsesWriter(t *testing.T) {
	list := testSessions(2)
	term := newFakeTerm(readline.KeyEvent{Code: readline.KeyDown}, readline.KeyEvent{Code: readline.KeyEnter})
	var buf syncBuf
	idx, ok, _ := pickSession(term, list, &buf, testSem())
	if !ok || idx != 1 {
		t.Fatalf("应确认第 2 项: idx=%d ok=%v", idx, ok)
	}
	got := buf.String()
	if !strings.Contains(got, "选择会话") || !strings.Contains(got, list[1].ID) {
		t.Errorf("picker 应写入注入 writer: %q", got)
	}
}

func TestPickerWindowFitsRows(t *testing.T) {
	list := testSessions(40)
	p := &sessionPicker{items: list, sem: testSem(), size: readline.Size{Cols: 100, Rows: 32}}
	var buf bytes.Buffer
	p.render(&buf)
	s := buf.String()
	if got, want := strings.Count(s, "\r\n"), 31; got != want {
		t.Errorf("窗口应写 1+30 行，实际 %d: %q", got, s)
	}
	if strings.Contains(s, list[30].ID) {
		t.Errorf("窗口外项不应渲染: %q", s)
	}
	if !strings.Contains(s, list[29].ID) || !strings.Contains(s, "选择会话（1/40，") {
		t.Errorf("窗口内容或位置指示不符: %q", s)
	}
}

func TestPickerLinesTrackedAndBounded(t *testing.T) {
	for _, rows := range []int{3, 5, 12, 24, 32} {
		list := testSessions(rows * 2)
		p := &sessionPicker{items: list, sem: testSem(), size: readline.Size{Cols: 80, Rows: rows}}
		var buf bytes.Buffer
		p.render(&buf)
		wrote := strings.Count(buf.String(), "\r\n")
		if p.lines != wrote {
			t.Errorf("rows=%d 记账 %d ≠ 实际写入 %d", rows, p.lines, wrote)
		}
		if p.lines > rows-1 {
			t.Errorf("rows=%d 块占 %d 行，超出屏幕", rows, p.lines)
		}
		p.cursor = len(list) - 1
		buf.Reset()
		p.render(&buf)
		if !strings.HasPrefix(buf.String(), term.CursorUp(wrote)) {
			t.Errorf("rows=%d 上移量应为上次行数 %d: %q", rows, wrote, buf.String())
		}
		if p.lines > rows-1 {
			t.Errorf("rows=%d 跟随光标后块超屏: %d 行", rows, p.lines)
		}
	}
}

func TestPickerWindowFollowsCursor(t *testing.T) {
	list := testSessions(40)
	p := &sessionPicker{items: list, cursor: 35, sem: testSem(), size: readline.Size{Cols: 100, Rows: 32}}
	var buf bytes.Buffer
	p.render(&buf)
	s := buf.String()
	if strings.Contains(s, list[5].ID) || !strings.Contains(s, list[6].ID) || !strings.Contains(s, list[35].ID) {
		t.Errorf("窗口应跟随光标（6..35）: %q", s)
	}
	p.cursor = 0
	buf.Reset()
	p.render(&buf)
	s = buf.String()
	if strings.Contains(s, list[30].ID) || !strings.Contains(s, list[0].ID) {
		t.Errorf("回到顶部时窗口应回到起始: %q", s)
	}
}

func TestPickerAllItemsWhenSizeUnknown(t *testing.T) {
	p := &sessionPicker{items: testSessions(40), sem: testSem()}
	var buf bytes.Buffer
	p.render(&buf)
	if got := strings.Count(buf.String(), "\r\n"); got != 41 {
		t.Errorf("尺寸不可知时应全量渲染 40 项，实际 %d 行", got)
	}
}

func TestPickerRowTruncatedToCols(t *testing.T) {
	list := testSessions(3)
	list[0].Summary = strings.Repeat("宽", 40)
	p := &sessionPicker{items: list, sem: testSem(), size: readline.Size{Cols: 30, Rows: 24}}
	var buf bytes.Buffer
	p.render(&buf)
	for _, line := range strings.Split(buf.String(), "\r\n") {
		if line == "" {
			continue
		}
		if w := term.Width(term.Strip(line)); w > 29 {
			t.Errorf("行宽 %d 超出 cols-1: %q", w, line)
		}
	}
}

func TestSessSummarySingleLine(t *testing.T) {
	got := sessSummary(agent.SessionInfo{Summary: "a\x1b[2Jb\nc"})
	if strings.ContainsAny(got, "\x1b\n") {
		t.Errorf("摘要应折成单行并清洗控制序列: %q", got)
	}
	if got != "ab c" {
		t.Errorf("摘要文本应保留: %q", got)
	}
	if arch := sessSummary(agent.SessionInfo{Archived: true, Summary: "s\x1b[31m"}); strings.Contains(arch, "\x1b") || !strings.HasPrefix(arch, SessArchMark) {
		t.Errorf("归档标记应保留且内容已清洗: %q", arch)
	}
}

type pickConsole struct {
	beginErr error
	events   []readline.Event
	sizes    []readline.Size
}

func (c *pickConsole) BeginRead() error                      { return c.beginErr }
func (c *pickConsole) EndRead()                              {}
func (c *pickConsole) Sane()                                 {}
func (c *pickConsole) Subscribe(func(readline.Event)) func() { return func() {} }

func (c *pickConsole) SubscribeKeys(func(readline.Event)) func() { return func() {} }

func (c *pickConsole) Size() (readline.Size, bool) {
	if len(c.sizes) == 0 {
		return readline.Size{}, false
	}
	s := c.sizes[0]
	if len(c.sizes) > 1 {
		c.sizes = c.sizes[1:]
	}
	return s, true
}

func (c *pickConsole) LendStdin() (readline.Lease, error) { return nil, readline.ErrUnsupported }

func (c *pickConsole) LendFull(*exec.Cmd, io.Writer) (readline.Lease, error) {
	return nil, readline.ErrUnsupported
}

func (c *pickConsole) ReadEvent() (readline.Event, error) {
	if len(c.events) == 0 {
		return readline.Event{}, io.EOF
	}
	ev := c.events[0]
	c.events = c.events[1:]
	return ev, nil
}

func keyEvent(code readline.KeyCode, r rune) readline.Event {
	return readline.Event{Kind: readline.EventKey, Key: readline.KeyEvent{Code: code, Rune: r}}
}

func TestPickSessionEmptyListFallsBack(t *testing.T) {
	var buf bytes.Buffer
	idx, ok, keys := pickSession(&pickConsole{}, nil, &buf, testSem())
	if idx != -1 || ok || !keys {
		t.Fatalf("空列表应直接回落数字选择: idx=%d ok=%v keys=%v", idx, ok, keys)
	}
	if buf.Len() != 0 {
		t.Errorf("空列表不应渲染选择器: %q", buf.String())
	}
}

func TestPickSessionWithoutKeys(t *testing.T) {
	var buf bytes.Buffer
	idx, ok, keys := pickSession(&pickConsole{beginErr: readline.ErrUnsupported}, testSessions(3), &buf, testSem())
	if idx != -1 || ok || keys {
		t.Fatalf("非逐键终端应回落数字选择: idx=%d ok=%v keys=%v", idx, ok, keys)
	}
	if buf.Len() != 0 {
		t.Errorf("回落路径不应渲染选择器: %q", buf.String())
	}
}

func TestPickSessionReadErrorAndInterrupt(t *testing.T) {
	cases := map[string]*pickConsole{
		"读尽": {},
		"中断": {events: []readline.Event{{Kind: readline.EventInterrupt}}},
	}
	for name, c := range cases {
		var buf bytes.Buffer
		idx, ok, keys := pickSession(c, testSessions(3), &buf, testSem())
		if idx != -1 || ok || !keys {
			t.Errorf("%s: 应取消并保留按键能力: idx=%d ok=%v keys=%v", name, idx, ok, keys)
		}
	}
}

func TestPickSessionCancelKey(t *testing.T) {
	c := &pickConsole{events: []readline.Event{keyEvent(readline.KeyRune, 'q')}}
	var buf bytes.Buffer
	idx, ok, keys := pickSession(c, testSessions(3), &buf, testSem())
	if idx != -1 || ok || !keys {
		t.Fatalf("q 应取消: idx=%d ok=%v keys=%v", idx, ok, keys)
	}
}

func TestPickSessionSizeChangeRedrawsWithoutStaleUp(t *testing.T) {
	c := &pickConsole{
		sizes: []readline.Size{{Cols: 100, Rows: 32}, {Cols: 60, Rows: 20}},
		events: []readline.Event{
			keyEvent(readline.KeyDown, 0),
			keyEvent(readline.KeyEnter, 0),
		},
	}
	var buf bytes.Buffer
	idx, ok, keys := pickSession(c, testSessions(3), &buf, testSem())
	if !ok || !keys || idx != 1 {
		t.Fatalf("idx=%d ok=%v keys=%v", idx, ok, keys)
	}
	if n := strings.Count(buf.String(), PickTitle[:len("选择会话")]); n != 2 {
		t.Errorf("尺寸变化后应重绘一次（共两帧），实际 %d 帧: %q", n, buf.String())
	}
	if re := regexp.MustCompile(`\x1b\[[0-9]*A`); re.MatchString(buf.String()) {
		t.Errorf("尺寸变化后不应按旧行数上移光标: %q", buf.String())
	}
}
