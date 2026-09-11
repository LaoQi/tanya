package repl

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/LaoQi/tanyan/readline"
)

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

func newTestREPL(t *testing.T, term readline.Terminal) (*REPL, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	st := NewStreams(out, errb)
	r, err := NewREPL(nil, "› ", WithStreams(st), WithTerminal(term, false))
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
	term.onKey = func() { r.st.out.emit("噪音") }
	if err := r.Run(); err != nil {
		t.Fatal(err)
	}
	if !violated {
		t.Error("输入期写入应被 guard 捕获")
	}
}
