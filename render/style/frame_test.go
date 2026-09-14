package style

import (
	"testing"

	"github.com/LaoQi/tanyan/render/term"
)

func setTestLevel(l term.ColorLevel) func() {
	old := term.GetProfile()
	term.SetProfile(term.Profile{TTY: l != term.LevelNone, Colors: l})
	return func() { term.SetProfile(old) }
}

func TestFrameStripsAndWraps(t *testing.T) {
	defer setTestLevel(term.Level16)()
	in := "a\x1b[31m红\x1b[0mb\x1b[2Kc\rd\x1b]0;t\x07e\tf"
	want := "\x1b[90m" + "a红bcde\tf" + "\x1b[0m"
	if got := (Style{Fg: Color16(8)}).Frame(in); got != want {
		t.Errorf("Frame = %q, want %q", got, want)
	}
}

func TestFrameNoColorProfile(t *testing.T) {
	defer setTestLevel(term.LevelNone)()
	if got := (Style{Fg: Color16(8)}).Frame("a\x1b[31mb\x1b[0m"); got != "ab" {
		t.Errorf("无色环境应退化为纯清洗: %q", got)
	}
}

func TestFrameEmptyStyle(t *testing.T) {
	defer setTestLevel(term.Level16)()
	if got := (Style{}).Frame("a\x1b[31mb"); got != "ab" {
		t.Errorf("空样式不包裹: %q", got)
	}
}
