package readline

import (
	"io"
	"syscall"
	"testing"
)

type scriptStep struct {
	data []byte
	err  error
}

type scriptReader struct {
	steps []scriptStep
	i     int
	hung  bool
}

func (s *scriptReader) readChunk(p []byte) (int, error) {
	if s.i >= len(s.steps) {
		return 0, nil
	}
	st := s.steps[s.i]
	s.i++
	if st.err != nil {
		return 0, st.err
	}
	return copy(p, st.data), nil
}

func (s *scriptReader) hungUp() bool { return s.hung }

func newScriptKeySource(steps ...scriptStep) *keySource {
	return &keySource{src: &scriptReader{steps: steps}}
}

func readKeys(t *testing.T, k *keySource, n int) []KeyEvent {
	t.Helper()
	out := make([]KeyEvent, 0, n)
	for i := 0; i < n; i++ {
		ev, err := k.readKey()
		if err != nil {
			t.Fatalf("第 %d 个按键失败: %v", i, err)
		}
		out = append(out, ev)
	}
	return out
}

func TestKeySourceReadsLine(t *testing.T) {
	k := newScriptKeySource(scriptStep{data: []byte("hi\r")})
	evs := readKeys(t, k, 3)
	if evs[0].Rune != 'h' || evs[1].Rune != 'i' || evs[2].Code != KeyEnter {
		t.Fatalf("got %+v", evs)
	}
}

func TestKeySourceEscapeFlushOnTimeout(t *testing.T) {
	k := newScriptKeySource(scriptStep{data: []byte{0x1b}}, scriptStep{})
	if ev := readKeys(t, k, 1)[0]; ev.Code != KeyEsc {
		t.Fatalf("超时后应把孤立 ESC 判为 Esc: %+v", ev)
	}
}

func TestKeySourceSplitSequence(t *testing.T) {
	k := newScriptKeySource(scriptStep{data: []byte("\x1b[")}, scriptStep{data: []byte("A")})
	if ev := readKeys(t, k, 1)[0]; ev.Code != KeyUp {
		t.Fatalf("跨 chunk 的方向键解析失败: %+v", ev)
	}
}

func TestKeySourceRetriesEINTR(t *testing.T) {
	k := newScriptKeySource(scriptStep{err: syscall.EINTR}, scriptStep{data: []byte("x")})
	if ev := readKeys(t, k, 1)[0]; ev.Rune != 'x' {
		t.Fatalf("EINTR 后应重试: %+v", ev)
	}
}

func TestKeySourceEOFOnEIO(t *testing.T) {
	k := newScriptKeySource(scriptStep{err: syscall.EIO})
	if _, err := k.readKey(); err != io.EOF {
		t.Fatalf("EIO 应归一为 io.EOF: %v", err)
	}
}

func TestKeySourceEOFOnHangUp(t *testing.T) {
	src := &scriptReader{steps: []scriptStep{{}}, hung: true}
	k := &keySource{src: src}
	if _, err := k.readKey(); err != io.EOF {
		t.Fatalf("挂断应返回 io.EOF: %v", err)
	}
}

func TestKeySourcePropagatesError(t *testing.T) {
	k := newScriptKeySource(scriptStep{err: io.EOF})
	if _, err := k.readKey(); err != io.EOF {
		t.Fatalf("底层错误应透传: %v", err)
	}
}

func TestKeySourceReset(t *testing.T) {
	k := newScriptKeySource()
	k.parser.feed([]byte("\x1b["))
	k.queue = append(k.queue, KeyEvent{Code: KeyUp})
	k.reset()
	if len(k.queue) != 0 {
		t.Errorf("reset 应清空队列: %+v", k.queue)
	}
	if k.parser.needsMore() {
		t.Error("reset 应清空解析器残片状态")
	}
}

func TestKeySourceDropsIncompleteRuneOnTimeout(t *testing.T) {
	k := newScriptKeySource(scriptStep{data: []byte{0xe4}}, scriptStep{}, scriptStep{data: []byte("中")})
	ev := readKeys(t, k, 1)[0]
	if ev.Code != KeyRune || ev.Rune != '中' {
		t.Fatalf("残片超时后应丢弃并继续读后续字符: %+v", ev)
	}
}

func TestKeySourceIncompleteRuneThenHangUp(t *testing.T) {
	src := &scriptReader{steps: []scriptStep{{data: []byte{0xe4}}, {}, {}}, hung: true}
	k := &keySource{src: src}
	if _, err := k.readKey(); err != io.EOF {
		t.Fatalf("残片丢弃后挂断应返回 io.EOF（不得越界 panic）: %v", err)
	}
}
