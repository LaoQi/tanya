package repl

import (
	"bytes"
	"github.com/LaoQi/tanyan/render/term"
	"github.com/LaoQi/tanyan/render/theme"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
)

// syncBuf 让测试断言与 spinner goroutine 的写入互斥，-race 下安全。
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// writeCounter 记录写入次数与内容，用于断言"整块一次写完"。
type writeCounter struct {
	mu sync.Mutex
	n  int
	b  bytes.Buffer
}

func (w *writeCounter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n++
	return w.b.Write(p)
}

func (w *writeCounter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

func (w *writeCounter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

func (b *syncBuf) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// fakeTerm 是 repl 侧的 readline.Terminal 替身：按键序列驱动 Run，无需真实终端。
type fakeTerm struct {
	keys  []readline.KeyEvent
	idx   int
	raw   bool
	inKey bool
	onKey func()
}

func newFakeTerm(keys ...readline.KeyEvent) *fakeTerm {
	return &fakeTerm{keys: keys}
}

func line(s string) readline.KeyEvent {
	return readline.KeyEvent{Code: readline.KeyLine, Text: s}
}

func (f *fakeTerm) Raw() error { f.raw = true; return nil }

func (f *fakeTerm) Restore() { f.raw = false }

func (f *fakeTerm) Size() (readline.Size, bool) { return readline.Size{Cols: 80, Rows: 24}, true }

func (f *fakeTerm) ReadKey() (readline.KeyEvent, error) {
	f.inKey = true
	defer func() { f.inKey = false }()
	if f.onKey != nil {
		f.onKey()
	}
	if f.idx >= len(f.keys) {
		return readline.KeyEvent{}, io.EOF
	}
	ev := f.keys[f.idx]
	f.idx++
	return ev, nil
}

func newTestREPLAgent(t *testing.T, a *agent.Agent, dev readline.Terminal) (*REPL, *syncBuf, *syncBuf) {
	t.Helper()
	out, errb := &syncBuf{}, &syncBuf{}
	st := NewStreams(out, errb, modeRich)
	r, err := NewREPL(a, "› ", WithStreams(st), WithTerminal(dev, false))
	if err != nil {
		t.Fatal(err)
	}
	return r, out, errb
}

func newTestREPL(t *testing.T, dev readline.Terminal) (*REPL, *syncBuf, *syncBuf) {
	return newTestREPLAgent(t, nil, dev)
}

// newTestREPLMode 以指定输出模式与 profile 构造（profile 需在建 REPL 之前设置：构造期会快照）。
func newTestREPLMode(t *testing.T, dev readline.Terminal, mode outMode, prof term.Profile) (*REPL, *syncBuf, *syncBuf) {
	t.Helper()
	out, errb := &syncBuf{}, &syncBuf{}
	st := NewStreams(out, errb, mode)
	old := term.GetProfile()
	term.SetProfile(prof)
	t.Cleanup(func() { term.SetProfile(old) })
	r, err := NewREPL(nil, "› ", WithStreams(st), WithTerminal(dev, false))
	if err != nil {
		t.Fatal(err)
	}
	return r, out, errb
}

func TestFakeTermDrivesRun(t *testing.T) {
	term := newFakeTerm(line(""), line("exit"))
	r, out, _ := newTestREPL(t, term)
	if err := r.Run(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "输入 /help 查看命令") {
		t.Errorf("欢迎屏应写入注入 writer: %q", got)
	}
	if !strings.Contains(got, MsgBye) {
		t.Errorf("退出文案应写入注入 writer: %q", got)
	}
	if n := strings.Count(got, "› "); n != 2 {
		t.Errorf("两轮输入应各写一次提示符，实际 %d: %q", n, got)
	}
	if term.raw {
		t.Error("非 raw 终端不应进入 raw 模式")
	}
}

func TestGuardNoEmitDuringInput(t *testing.T) {
	term := newFakeTerm(line("exit"))
	r, _, _ := newTestREPL(t, term)
	var emits int
	var violated bool
	r.st.out.guard = func() {
		emits++
		if term.inKey {
			violated = true
		}
	}
	if err := r.Run(); err != nil {
		t.Fatal(err)
	}
	if violated {
		t.Error("输入期不应有 emit 写入")
	}
	if emits == 0 {
		t.Error("guard 未被触发，断言机制失效")
	}
}

func TestGuardCatchesEmitDuringInput(t *testing.T) {
	term := newFakeTerm(line("exit"))
	r, _, _ := newTestREPL(t, term)
	var violated bool
	r.st.out.guard = func() {
		if term.inKey {
			violated = true
		}
	}
	term.onKey = func() { r.st.out.emit(KindContent, "噪音") }
	if err := r.Run(); err != nil {
		t.Fatal(err)
	}
	if !violated {
		t.Error("输入期写入应被 guard 捕获")
	}
}

func testSem() theme.Semantics { return Semantics("default", nil) }
