package readline

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func stubExitRequested(t *testing.T, fn func() bool) {
	t.Helper()
	old := exitRequested
	exitRequested = fn
	t.Cleanup(func() { exitRequested = old })
}

func TestKeySourceStopsOnExitRequest(t *testing.T) {
	stubExitRequested(t, func() bool { return true })
	k := newScriptKeySource(scriptStep{data: []byte("hi\r")})
	if _, err := k.readKey(); err != ErrExited {
		t.Fatalf("got %v", err)
	}
}

func TestKeySourceExitBeatsQueuedInput(t *testing.T) {
	requested := false
	stubExitRequested(t, func() bool { return requested })
	k := newScriptKeySource(scriptStep{data: []byte("ab\r")})
	if ev, err := k.readKey(); err != nil || ev.Rune != 'a' {
		t.Fatalf("首个按键: %+v %v", ev, err)
	}
	requested = true
	if _, err := k.readKey(); err != ErrExited {
		t.Fatalf("积压输入不应拖延退出: %v", err)
	}
}

func TestDegradedStopsOnExitRequest(t *testing.T) {
	stubExitRequested(t, func() bool { return true })
	d := &Degraded{r: bufio.NewReader(strings.NewReader("x\n"))}
	if _, err := d.ReadKey(); err != ErrExited {
		t.Fatalf("got %v", err)
	}
}

func TestEditorStopsOnExitRequest(t *testing.T) {
	stubExitRequested(t, func() bool { return true })
	f := &fakeTerm{events: []KeyEvent{{Code: KeyLine, Text: "x"}}, out: &bytes.Buffer{}}
	ed := NewEditor(f, true)
	if _, err := ed.Readline("› "); err != ErrExited {
		t.Fatalf("got %v", err)
	}
	if f.rawCalls != 0 {
		t.Errorf("退出请求下不应切换 raw: %d", f.rawCalls)
	}
	if f.raw {
		t.Error("退出请求下不应停在 raw")
	}
}
