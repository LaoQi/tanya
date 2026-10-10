package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAskSanesTerminalAndSubscribes(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	term := newFakeTerm()
	r, out, errb := newTestREPLAgent(t, a, term)
	r.ask("你好")
	if term.sane == 0 {
		t.Error("回合前应对终端做纯模式自愈（Console.Sane）")
	}
	if term.subs == 0 {
		t.Error("回合应订阅 Console 中断事件")
	}
	if errb.String() == "" {
		t.Errorf("单发失败应落错误行: out=%q err=%q", out.String(), errb.String())
	}
}

func TestAskEmitsRefEcho(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newSessTestAgent(t, dir)
	term := newFakeTerm()
	r, out, _ := newTestREPLAgent(t, a, term)
	r.ask("看一下 @" + path)
	if !strings.Contains(out.String(), "[引用 note.txt 5B]") {
		t.Errorf("成功引用应回显一行: %q", out.String())
	}

	out.Reset()
	r.ask("看一下 @" + filepath.Join(dir, "gone.txt"))
	if strings.Contains(out.String(), "[引用") {
		t.Errorf("不存在的路径不应回显: %q", out.String())
	}
}
