package repl

import (
	"io"
	"testing"

	"github.com/LaoQi/tanya/readline"
)

type widthTerm struct{ cols int }

func (t widthTerm) BeginRead() error                         { return nil }
func (t widthTerm) EndRead()                                 {}
func (t widthTerm) Size() (readline.Size, bool)              { return readline.Size{Cols: t.cols, Rows: 24}, true }
func (t widthTerm) Subscribe(fn func(readline.Event)) func() { return func() {} }
func (t widthTerm) ReadEvent() (readline.Event, error) {
	return readline.Event{}, io.EOF
}

func newTermFactsREPL(t *testing.T, dev readline.Console, opts ...Option) *REPL {
	t.Helper()
	out, errb := &syncBuf{}, &syncBuf{}
	st := NewStreams(out, errb, modeRich)
	all := append([]Option{WithStreams(st), WithConsole(dev)}, opts...)
	r, err := NewREPL(nil, "› ", all...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestREPLUsesInjectedTermFacts(t *testing.T) {
	r := newTermFactsREPL(t, widthTerm{cols: 40}, WithTermFacts(TermFacts{Cols: 120, ColsOK: true}))
	if got := r.view.width(); got != 120 {
		t.Errorf("注入宽度应生效: %d", got)
	}
}

func TestREPLFallsBackToTerminalSize(t *testing.T) {
	r := newTermFactsREPL(t, widthTerm{cols: 40})
	if got := r.view.width(); got != 40 {
		t.Errorf("未注入时应回落到终端尺寸: %d", got)
	}
}

func TestREPLTermFactsWithoutSize(t *testing.T) {
	r := newTermFactsREPL(t, widthTerm{cols: 40}, WithTermFacts(TermFacts{Cols: 0, ColsOK: false}))
	if got := r.view.width(); got != defaultToolWidth {
		t.Errorf("无尺寸应回落默认宽度: %d", got)
	}
}
