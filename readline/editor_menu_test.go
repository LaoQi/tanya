package readline

import (
	"strings"
	"testing"
)

func TestTabMenuHighlightAndInsert(t *testing.T) {
	ed, _, out := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyEnter}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetOutput(out)
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/xyz"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/sessions" {
		t.Fatalf("首项应默认选中并插入: %q %v", line, err)
	}
	if !strings.Contains(out.String(), "\x1b[7m/sessions\x1b[0m") {
		t.Errorf("选中项应反显高亮: %q", out.String())
	}
}

func TestTabMenuDownUpNavigate(t *testing.T) {
	ed, _, out := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyDown}, KeyEvent{Code: KeyEnter}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetOutput(out)
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/xyz"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/xyz" {
		t.Fatalf("Down 应选中第二项并插入: %q %v", line, err)
	}
	if !strings.Contains(out.String(), "\x1b[7m/xyz\x1b[0m") {
		t.Errorf("Down 后高亮应移至第二项: %q", out.String())
	}
}

func TestTabMenuUpWraps(t *testing.T) {
	ed, _, _ := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyUp}, KeyEvent{Code: KeyEnter}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/xyz"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/xyz" {
		t.Fatalf("Up 在首项应回绕到末项: %q %v", line, err)
	}
}

func TestTabMenuTabCycles(t *testing.T) {
	ed, _, _ := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyEnter}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/xyz"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/sessions" {
		t.Fatalf("Tab 循环两次后应回到首项: %q %v", line, err)
	}
}

func TestTabMenuEscCloses(t *testing.T) {
	ed, _, _ := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyEsc}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/xyz"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/s" {
		t.Fatalf("Esc 应关闭菜单且 Enter 提交原文: %q %v", line, err)
	}
}

func TestTabMenuTypingCloses(t *testing.T) {
	ed, _, _ := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyRune, Rune: 'e'}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/xyz"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/se" {
		t.Fatalf("打字应关闭菜单并正常插入: %q %v", line, err)
	}
}

func TestTabMenuUpDownNotHistory(t *testing.T) {
	evs := append(runes("prev"), KeyEvent{Code: KeyEnter})
	evs = append(evs, runes("/s")...)
	evs = append(evs, KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyDown}, KeyEvent{Code: KeyEnter}, KeyEvent{Code: KeyEnter})
	ed, _, _ := newFakeEditor(evs...)
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/xyz"}}
		}
		return nil
	})
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	line, err := ed.Readline("> ")
	if err != nil || line != "/xyz" {
		t.Fatalf("菜单打开时 Down 不应触发历史导航: %q %v", line, err)
	}
}

func TestTabMenuScrollWindow(t *testing.T) {
	ed, _, out := newFakeEditor(
		append(runes("/c"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyEsc}, KeyEvent{Code: KeyEnter})...,
	)
	ed.SetOutput(out)
	ed.SetComplete(func(line string) []Completion {
		if line == "/c" {
			var cands []Completion
			for i := 0; i < 10; i++ {
				cands = append(cands, Completion{Insert: "/c" + string(rune('0'+i))})
			}
			return cands
		}
		return nil
	})
	if _, err := ed.Readline("> "); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for i := 0; i < 8; i++ {
		if !strings.Contains(s, "/c"+string(rune('0'+i))) {
			t.Errorf("窗口内应含 /c%d", i)
		}
	}
	if strings.Contains(s, "/c8") || strings.Contains(s, "/c9") {
		t.Errorf("超出窗口的候选不应渲染: %q", s)
	}
}

func TestTabMenuNoSize(t *testing.T) {
	ed, _, _ := newFakeEditor(
		append(runes("/s"), KeyEvent{Code: KeyTab}, KeyEvent{Code: KeyDown}, KeyEvent{Code: KeyEnter})...,
	)
	ed.term = noSizeTerm{ed.term}
	ed.SetComplete(func(line string) []Completion {
		if line == "/s" {
			return []Completion{{Insert: "/sessions"}, {Insert: "/xyz"}}
		}
		return nil
	})
	line, err := ed.Readline("> ")
	if err != nil || line != "/s" {
		t.Fatalf("Size 不可用不应打开菜单（Down 保持历史导航语义）: %q %v", line, err)
	}
}

type noSizeTerm struct{ Terminal }

func (noSizeTerm) Size() (Size, bool) { return Size{}, false }
