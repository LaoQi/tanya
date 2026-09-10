package readline

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeTerm struct {
	events   []KeyEvent
	out      *bytes.Buffer
	raw      bool
	rawCalls int
	cols     int
}

func (f *fakeTerm) Raw() error { f.raw = true; f.rawCalls++; return nil }
func (f *fakeTerm) Restore()   { f.raw = false }
func (f *fakeTerm) Size() (Size, bool) {
	cols := f.cols
	if cols <= 0 {
		cols = 80
	}
	return Size{Cols: cols, Rows: 24}, true
}
func (f *fakeTerm) ReadKey() (KeyEvent, error) {
	if len(f.events) == 0 {
		return KeyEvent{}, io.EOF
	}
	ev := f.events[0]
	f.events = f.events[1:]
	return ev, nil
}

func newFakeEditor(events ...KeyEvent) (*Editor, *fakeTerm, *bytes.Buffer) {
	f := &fakeTerm{events: events, out: &bytes.Buffer{}}
	return NewEditor(f, true), f, f.out
}

func runes(s string) []KeyEvent {
	var evs []KeyEvent
	for _, r := range s {
		evs = append(evs, KeyEvent{Code: KeyRune, Rune: r})
	}
	return evs
}

func TestEditorTypeAndEnter(t *testing.T) {
	ed, _, _ := newFakeEditor(append(runes("hello"), KeyEvent{Code: KeyEnter})...)
	line, err := ed.Readline("> ")
	if err != nil || line != "hello" {
		t.Fatalf("line=%q err=%v", line, err)
	}
	if len(ed.History()) != 1 || ed.History()[0] != "hello" {
		t.Errorf("history: %v", ed.History())
	}
}

func TestEditorBackspaceAndInsert(t *testing.T) {
	var evs []KeyEvent
	evs = append(evs, runes("abc")...)
	evs = append(evs, KeyEvent{Code: KeyLeft}, KeyEvent{Code: KeyBackspace}, KeyEvent{Code: KeyEnd})
	evs = append(evs, runes("d")...)
	evs = append(evs, KeyEvent{Code: KeyEnter})
	ed, _, _ := newFakeEditor(evs...)
	line, err := ed.Readline("> ")
	if err != nil || line != "acd" {
		t.Fatalf("line=%q err=%v", line, err)
	}
}

func TestEditorHistoryNav(t *testing.T) {
	ed, _, _ := newFakeEditor(KeyEvent{Code: KeyEnter})
	ed.History() // noop
	ed.history = []string{"first", "second"}
	ed.histIdx = 2

	evs := []KeyEvent{
		{Code: KeyUp}, {Code: KeyUp},
		{Code: KeyEnter},
	}
	ed2, _, _ := newFakeEditor(evs...)
	ed2.history = ed.history
	ed2.histIdx = 2
	line, err := ed2.Readline("> ")
	if err != nil || line != "first" {
		t.Fatalf("line=%q err=%v", line, err)
	}

	evs2 := []KeyEvent{{Code: KeyUp}, {Code: KeyDown}, {Code: KeyEnter}}
	ed3, _, _ := newFakeEditor(evs2...)
	ed3.history = ed.history
	ed3.histIdx = 2
	line, err = ed3.Readline("> ")
	if err != nil || line != "" {
		t.Fatalf("回到末尾应恢复空 draft: %q %v", line, err)
	}
}

func TestEditorCtrlCAndCtrlD(t *testing.T) {
	ed, _, _ := newFakeEditor(KeyEvent{Code: KeyCtrlC})
	if _, err := ed.Readline("> "); !errors.Is(err, ErrInterrupt) {
		t.Fatalf("Ctrl+C: %v", err)
	}
	ed2, _, _ := newFakeEditor(KeyEvent{Code: KeyCtrlD})
	if _, err := ed2.Readline("> "); !errors.Is(err, io.EOF) {
		t.Fatalf("空行 Ctrl+D: %v", err)
	}
	ed3, _, _ := newFakeEditor(append(runes("x"), KeyEvent{Code: KeyLeft}, KeyEvent{Code: KeyCtrlD}, KeyEvent{Code: KeyEnter})...)
	line, err := ed3.Readline("> ")
	if err != nil || line != "" {
		t.Fatalf("非空行 Ctrl+D 应删除光标处字符: %q %v", line, err)
	}
}

func TestEditorRawModeLifecycle(t *testing.T) {
	ed, f, _ := newFakeEditor(KeyEvent{Code: KeyEnter})
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	if f.rawCalls == 0 {
		t.Error("Readline 应进入 raw 模式")
	}
	if f.raw {
		t.Error("Readline 返回后应退出 raw 模式")
	}
}

func TestEditorRenderOutput(t *testing.T) {
	ed, _, out := newFakeEditor(append(runes("hi"), KeyEvent{Code: KeyEnter})...)
	ed.SetOutput(out)
	if _, err := ed.Readline("\033[32mP>\033[0m "); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "P>") || !strings.Contains(s, "hi") {
		t.Errorf("输出缺少内容: %q", s)
	}
}

func TestEditorDegraded(t *testing.T) {
	f := &fakeTerm{events: []KeyEvent{{Code: KeyLine, Text: "piped input"}}, out: &bytes.Buffer{}}
	ed := NewEditor(f, false)
	line, err := ed.Readline("> ")
	if err != nil || line != "piped input" {
		t.Fatalf("降级模式: %q %v", line, err)
	}
}

func readLine(t *testing.T, evs ...KeyEvent) string {
	t.Helper()
	ed, _, _ := newFakeEditor(append(evs, KeyEvent{Code: KeyEnter})...)
	line, err := ed.Readline("> ")
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func TestEditorCtrlUAndCtrlK(t *testing.T) {
	evs := append(runes("hello"), KeyEvent{Code: KeyCtrlU})
	if got := readLine(t, evs...); got != "" {
		t.Errorf("Ctrl+U 应删除光标前全部: %q", got)
	}
	evs2 := append(runes("hello"), KeyEvent{Code: KeyHome}, KeyEvent{Code: KeyCtrlF}, KeyEvent{Code: KeyCtrlK})
	if got := readLine(t, evs2...); got != "h" {
		t.Errorf("Ctrl+K 应删除光标到行尾: %q", got)
	}
}

func TestEditorCtrlWAndCtrlY(t *testing.T) {
	evs := append(runes("ab cd"), KeyEvent{Code: KeyCtrlW})
	if got := readLine(t, evs...); got != "ab " {
		t.Errorf("Ctrl+W 应删前一个词: %q", got)
	}
	evs2 := append(runes("ab cd"), KeyEvent{Code: KeyCtrlW}, KeyEvent{Code: KeyCtrlY})
	if got := readLine(t, evs2...); got != "ab cd" {
		t.Errorf("Ctrl+Y 应粘贴 kill 缓冲: %q", got)
	}
	evs3 := append(runes("hello"), KeyEvent{Code: KeyCtrlU}, KeyEvent{Code: KeyCtrlY})
	if got := readLine(t, evs3...); got != "hello" {
		t.Errorf("Ctrl+U 后 Ctrl+Y 应恢复: %q", got)
	}
}

func TestEditorWordMotion(t *testing.T) {
	evs := append(append(runes("ab cd"), KeyEvent{Code: KeyAltB}), runes("X")...)
	if got := readLine(t, evs...); got != "ab Xcd" {
		t.Errorf("Alt+B 应移到当前词首再插入: %q", got)
	}
	evs2 := append(append(append(runes("ab cd"), KeyEvent{Code: KeyCtrlA}), KeyEvent{Code: KeyAltF}), runes("X")...)
	if got := readLine(t, evs2...); got != "abX cd" {
		t.Errorf("Alt+F 应移到词尾再插入: %q", got)
	}
}

func TestEditorCtrlT(t *testing.T) {
	if got := readLine(t, append(runes("ab"), KeyEvent{Code: KeyCtrlT})...); got != "ba" {
		t.Errorf("行尾 Ctrl+T 应交换前两个字符: %q", got)
	}
	evs := append(runes("abc"), KeyEvent{Code: KeyCtrlB}, KeyEvent{Code: KeyCtrlT})
	if got := readLine(t, evs...); got != "acb" {
		t.Errorf("行中 Ctrl+T 应交换光标前后字符: %q", got)
	}
}

func TestEditorCtrlBFAndCtrlL(t *testing.T) {
	evs := append(append(append(runes("abc"), KeyEvent{Code: KeyCtrlA}), KeyEvent{Code: KeyCtrlF}), runes("X")...)
	if got := readLine(t, evs...); got != "aXbc" {
		t.Errorf("Ctrl+F 应右移一字符: %q", got)
	}
	ed, _, out := newFakeEditor(append(runes("hi"), KeyEvent{Code: KeyCtrlL}, KeyEvent{Code: KeyEnter})...)
	ed.SetOutput(out)
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	o := out.String()
	if !strings.Contains(o, strings.Repeat("\n", 48)) || !strings.Contains(o, "\x1b[23A") {
		t.Errorf("Ctrl+L 应推屏保历史（48 换行+上移 23 行）: %q", o)
	}
	if strings.Contains(o, "\x1b[2J") {
		t.Errorf("Ctrl+L 不应擦屏: %q", o)
	}
}

func TestEditorRenderWrap(t *testing.T) {
	f := &fakeTerm{cols: 10, out: &bytes.Buffer{}}
	f.events = append(runes("abcdefghij"), KeyEvent{Code: KeyEnter})
	ed := NewEditor(f, true)
	ed.SetOutput(f.out)
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "\r\x1b[1A\x1b[J") {
		t.Errorf("换行后重渲染应上移并清屏: %q", f.out.String())
	}
	if f.out.String() == "" {
		t.Fatal("无输出")
	}
}

func TestEditorRenderWrapCursorHome(t *testing.T) {
	ft := &fakeTerm{cols: 10, out: &bytes.Buffer{}}
	ft.events = append(runes("abcdefghij"), KeyEvent{Code: KeyHome}, KeyEvent{Code: KeyEnter})
	ed := NewEditor(ft, true)
	ed.SetOutput(ft.out)
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ft.out.String(), "\x1b[1A\r\x1b[2C") {
		t.Errorf("Home 后光标应上移一行定位到 prompt 后: %q", ft.out.String())
	}
}

func TestEditorRenderExactCols(t *testing.T) {
	ft := &fakeTerm{cols: 10, out: &bytes.Buffer{}}
	ft.events = append(runes("abcdefghijkl"), KeyEvent{Code: KeyEnter})
	ed := NewEditor(ft, true)
	ed.SetOutput(ft.out)
	if _, err := ed.Readline("12345678"); err != nil {
		t.Fatal(err)
	}
	out := ft.out.String()
	if !strings.Contains(out, "\r\x1b[1A\x1b[J") {
		t.Errorf("整列数倍行数应正确跟踪: %q", out)
	}
}

func TestEditorRenderWideWrapCursor(t *testing.T) {
	ft := &fakeTerm{cols: 10, out: &bytes.Buffer{}}
	ed := NewEditor(ft, true)
	ed.SetOutput(ft.out)
	ed.buf = []rune("abcdefghi中")
	ed.pos = len(ed.buf)
	ed.render("")
	if !strings.HasSuffix(ft.out.String(), "\r\x1b[2C") {
		t.Errorf("宽字符跨界后光标列应为 2: %q", ft.out.String())
	}
}
