package repl

import (
	"io"
	"os/exec"
	"testing"

	"github.com/LaoQi/tanya/readline"
)

type widthTerm struct {
	cols   int
	noSize bool
}

func (t widthTerm) BeginRead() error { return nil }
func (t widthTerm) EndRead()         {}
func (t widthTerm) Sane()            {}
func (t widthTerm) Size() (readline.Size, bool) {
	if t.noSize {
		return readline.Size{}, false
	}
	return readline.Size{Cols: t.cols, Rows: 24}, true
}
func (t widthTerm) Subscribe(fn func(readline.Event)) func() { return func() {} }

func (t widthTerm) SubscribeKeys(fn func(readline.Event)) func() { return func() {} }

func (t widthTerm) LendStdin() (readline.Lease, error) { return nil, readline.ErrUnsupported }

func (t widthTerm) LendFull(cmd *exec.Cmd, capture io.Writer) (readline.Lease, error) {
	return nil, readline.ErrUnsupported
}
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

func TestREPLWidthFollowsLiveSize(t *testing.T) {
	dev := &widthTerm{cols: 40}
	r := newTermFactsREPL(t, dev, WithTermFacts(TermFacts{Cols: 120, ColsOK: true}))
	if got := r.view.width(); got != 40 {
		t.Errorf("终端尺寸应胜过启动探测值: %d", got)
	}
	dev.cols = 60
	if got := r.view.width(); got != 60 {
		t.Errorf("宽度应现取（缩放后跟随）: %d", got)
	}
}

func TestREPLWidthFallsBackToFacts(t *testing.T) {
	r := newTermFactsREPL(t, &widthTerm{cols: 40, noSize: true}, WithTermFacts(TermFacts{Cols: 120, ColsOK: true}))
	if got := r.view.width(); got != 120 {
		t.Errorf("取不到终端尺寸时应回落注入值: %d", got)
	}
}

func TestREPLWidthFallsBackToDefault(t *testing.T) {
	r := newTermFactsREPL(t, &widthTerm{cols: 40, noSize: true})
	if got := r.view.width(); got != defaultToolWidth {
		t.Errorf("都取不到时应回落默认宽度: %d", got)
	}
}

func TestLiveWidthNilConsole(t *testing.T) {
	if got := LiveWidth(nil, TermFacts{Cols: 100, ColsOK: true})(); got != 100 {
		t.Errorf("无 Console 应用注入值: %d", got)
	}
	if got := LiveWidth(nil, TermFacts{})(); got != defaultToolWidth {
		t.Errorf("都无则用默认宽度: %d", got)
	}
}
