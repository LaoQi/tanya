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
