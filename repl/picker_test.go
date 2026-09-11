package repl

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/readline"
)

func testSessions(n int) []agent.SessionInfo {
	var list []agent.SessionInfo
	for i := 0; i < n; i++ {
		list = append(list, agent.SessionInfo{ID: "2026010" + string(rune('1'+i)) + "-100000"})
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
	p2.handle(readline.KeyEvent{Code: readline.KeyCtrlC})
	if !p2.done || !p2.cancel {
		t.Error("Ctrl+C 应取消")
	}
	p3 := &sessionPicker{items: testSessions(3)}
	p3.handle(readline.KeyEvent{Code: readline.KeyRune, Rune: 'q'})
	if !p3.done || !p3.cancel {
		t.Error("q 应取消")
	}
}

func TestPickerRender(t *testing.T) {
	p := &sessionPicker{items: testSessions(2), cursor: 1}
	var buf bytes.Buffer
	p.render(&buf, true)
	s := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte(PickTitle)) {
		t.Errorf("缺标题: %q", s)
	}
	count := 0
	for _, c := range s {
		_ = c
		count++
	}
	if !bytes.Contains(buf.Bytes(), []byte("\x1b[32m> \x1b[0m20260102-100000")) {
		t.Errorf("当前项应有高亮标记: %q", s)
	}
	var buf2 bytes.Buffer
	p.render(&buf2, false)
	if !bytes.Contains(buf2.Bytes(), []byte("\x1b[3A")) {
		t.Errorf("重绘应上移光标: %q", buf2.String())
	}
}

func TestPickerUsesWriter(t *testing.T) {
	list := testSessions(2)
	term := newFakeTerm(readline.KeyEvent{Code: readline.KeyDown}, readline.KeyEvent{Code: readline.KeyEnter})
	var buf syncBuf
	idx, ok := pickSession(term, list, &buf)
	if !ok || idx != 1 {
		t.Fatalf("应确认第 2 项: idx=%d ok=%v", idx, ok)
	}
	got := buf.String()
	if !strings.Contains(got, PickTitle) || !strings.Contains(got, list[1].ID) {
		t.Errorf("picker 应写入注入 writer: %q", got)
	}
}
