package readline

import (
	"strings"
	"testing"
)

func ghostEvents(t *testing.T, seq string, ghostFn func(string) string) (string, string) {
	t.Helper()
	evs := append(runes(seq), KeyEvent{Code: KeyEnter})
	ed, _, out := newFakeEditor(evs...)
	ed.SetOutput(out)
	ed.SetGhost(ghostFn)
	line, _ := ed.Readline("> ")
	return line, out.String()
}

func TestGhostSuggestionRendered(t *testing.T) {
	_, out := ghostEvents(t, "/h", func(line string) string {
		if line == "/h" {
			return "elp"
		}
		return ""
	})
	if !strings.Contains(out, "\x1b[90melp\x1b[0m") {
		t.Errorf("ghost 应以置灰渲染: %q", out)
	}
}

func TestGhostNotInResult(t *testing.T) {
	line, _ := ghostEvents(t, "/h", func(line string) string {
		if line == "/h" {
			return "elp"
		}
		return ""
	})
	if line != "/h" {
		t.Errorf("Enter 返回值不应包含 ghost: %q", line)
	}
}

func TestGhostTypingConsumes(t *testing.T) {
	var outs []string
	ed, _, out := newFakeEditor(append(runes("/hel"), KeyEvent{Code: KeyEnter})...)
	ed.SetOutput(out)
	ed.SetGhost(func(line string) string {
		outs = append(outs, line)
		switch line {
		case "/":
			return "help"
		case "/h":
			return "elp"
		case "/he":
			return "lp"
		case "/hel":
			return "p"
		}
		return ""
	})
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	if len(outs) < 4 || outs[0] != "/" || outs[1] != "/h" || outs[2] != "/he" || outs[3] != "/hel" {
		t.Errorf("ghost 状态机应随输入剥离重算: %v", outs)
	}
}

func TestGhostAcceptWithRight(t *testing.T) {
	ed, _, _ := newFakeEditor(
		append(runes("/l"), KeyEvent{Code: KeyRight}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetGhost(func(line string) string {
		if line == "/l" {
			return "oad"
		}
		return ""
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/load" {
		t.Fatalf("→ 应接受 ghost: %q %v", line, err)
	}
}

func TestTabSingleCandidate(t *testing.T) {
	ed, _, _ := newFakeEditor(
		append(runes("/lo"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetComplete(func(line string) []Completion {
		if line == "/lo" {
			return []Completion{{Insert: "/load"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/load" {
		t.Fatalf("单候选应直接补全: %q %v", line, err)
	}
}

func TestTabCommonPrefix(t *testing.T) {
	ed, _, _ := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/set"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/se" {
		t.Fatalf("多候选应先补公共前缀: %q %v", line, err)
	}
}

func TestTabListCandidates(t *testing.T) {
	ed, _, out := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyEnter}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetOutput(out)
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/xyz", Display: "/xyz  说明"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil {
		t.Fatal(err)
	}
	if line != "/sessions" {
		t.Errorf("菜单打开时 Enter 应插入选中项而非提交: %q", line)
	}
	if !strings.Contains(out.String(), "/sessions") || !strings.Contains(out.String(), "/xyz") {
		t.Errorf("无公共前缀时应列出候选: %q", out.String())
	}
}

func TestCommonPrefix(t *testing.T) {
	if got := commonPrefix([]string{"/sessions", "/set"}); got != "/se" {
		t.Errorf("got %q", got)
	}
	if got := commonPrefix([]string{"a", "b"}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestGhostClearedMidLine(t *testing.T) {
	ed, _, out := newFakeEditor(
		KeyEvent{Code: KeyLeft},
		KeyEvent{Code: KeyEnter},
	)
	ed.SetGhost(func(line string) string { return "xx" })
	ed.buf = []rune("ab")
	ed.pos = 2
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "xx") {
		t.Errorf("光标不在行尾时不应渲染 ghost: %q", out.String())
	}
}

func enteredScreen(t *testing.T, cols int, evs []KeyEvent, ghost func(string) string) *vtScreen {
	t.Helper()
	ed, f, out := newFakeEditor(evs...)
	f.cols = cols
	ed.SetOutput(out)
	ed.SetGhost(ghost)
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	scr := newVTScreen(cols, 24)
	scr.feed(out.String())
	return scr
}

func TestGhostClearedAfterEnter(t *testing.T) {
	scr := enteredScreen(t, 80, append(runes("/h"), KeyEvent{Code: KeyEnter}), func(line string) string {
		if line == "/h" {
			return "elp"
		}
		return ""
	})
	if got := scr.line(0); got != "> /h" {
		t.Errorf("回车后不应残留 ghost: %q", got)
	}
	if got := scr.line(1); got != "" {
		t.Errorf("回车后光标所在行应为空: %q", got)
	}
}

func TestGhostClearedAcrossWrap(t *testing.T) {
	scr := enteredScreen(t, 20, append(runes("/abcdefghijklmnop"), KeyEvent{Code: KeyEnter}), func(string) string {
		return "qrstuvwxyz0123456789"
	})
	if got := scr.line(0); got != "> /abcdefghijklmnop" {
		t.Errorf("wrap 场景第一行不应残留 ghost: %q", got)
	}
	if got := scr.line(1); got != "" {
		t.Errorf("wrap 场景溢出的物理行应被清除: %q", got)
	}
}

func TestShrinkClearsTailRows(t *testing.T) {
	evs := runes("abcdefghijklmnopqrstuvwxyz0123")
	for i := 0; i < 20; i++ {
		evs = append(evs, KeyEvent{Code: KeyBackspace})
	}
	scr := enteredScreen(t, 20, append(evs, KeyEvent{Code: KeyEnter}), nil)
	if got := scr.line(0); got != "> abcdefghij" {
		t.Errorf("退格后应重绘为单行: %q", got)
	}
	if got := scr.line(1); got != "" {
		t.Errorf("退格后多余的物理行应被清除: %q", got)
	}
}

func TestMenuRowsClearedOnEsc(t *testing.T) {
	ed, f, out := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyEsc}, KeyEvent{Code: KeyEnter})...,
	)
	f.cols = 40
	ed.SetOutput(out)
	ed.SetComplete(func(line string) []Completion {
		return []Completion{{Insert: "/sessions"}, {Insert: "/set"}}
	})
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	scr := newVTScreen(40, 24)
	scr.feed(out.String())
	if got := scr.line(0); got != "> /se" {
		t.Errorf("Esc 关闭菜单后不应残留候选行: %q", got)
	}
	if got := scr.line(1); got != "" {
		t.Errorf("菜单行应被清除: %q", got)
	}
}

func TestCtrlCClearsGhost(t *testing.T) {
	ed, f, out := newFakeEditor(append(runes("/h"), KeyEvent{Code: KeyCtrlC})...)
	f.cols = 40
	ed.SetOutput(out)
	ed.SetGhost(func(string) string { return "elp" })
	if _, err := ed.Readline("> "); err != ErrInterrupt {
		t.Fatalf("^C 应返回 ErrInterrupt: %v", err)
	}
	scr := newVTScreen(40, 24)
	scr.feed(out.String())
	if got := scr.line(0); got != ">" {
		t.Errorf("^C 后该行应清空: %q", got)
	}
}

func TestLongInputDoesNotFlushBlanks(t *testing.T) {
	evs := runes(strings.Repeat("x", 300))
	evs = append(evs,
		KeyEvent{Code: KeyBackspace},
		KeyEvent{Code: KeyRune, Rune: 'y'},
		KeyEvent{Code: KeyEnter},
	)
	ed, f, out := newFakeEditor(evs...)
	f.cols = 20
	f.rows = 6
	ed.SetOutput(out)
	ed.SetGhost(func(string) string { return "" })
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	scr := newVTScreen(20, 6)
	scr.feed(out.String())
	blanks := 0
	for _, l := range scr.scrollback {
		if l == "" {
			blanks++
		}
	}
	if blanks > 0 {
		t.Errorf("块高于屏幕时清行循环不应把空白行推出屏幕: %d 行滚出（共 %d）", blanks, len(scr.scrollback))
	}
}
