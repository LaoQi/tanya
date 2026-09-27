package style

import (
	"testing"

	"github.com/LaoQi/tanya/render/term"
)

func withLevel(l term.ColorLevel) term.Profile {
	return term.Profile{TTY: l != term.LevelNone, Colors: l}
}

func TestFrameStripsAndWraps(t *testing.T) {
	p := withLevel(term.Level16)
	in := "a\x1b[31m红\x1b[0mb\x1b[2Kc\rd\x1b]0;t\x07e\tf"
	want := "\x1b[90m" + "a红bcde\tf" + "\x1b[0m"
	if got := (Style{Fg: Color16(8)}).With(p).Frame(in); got != want {
		t.Errorf("Frame = %q, want %q", got, want)
	}
}

func TestFrameNoColorProfile(t *testing.T) {
	p := withLevel(term.LevelNone)
	if got := (Style{Fg: Color16(8)}).With(p).Frame("a\x1b[31mb\x1b[0m"); got != "ab" {
		t.Errorf("无色环境应退化为纯清洗: %q", got)
	}
}

func TestFrameEmptyStyle(t *testing.T) {
	p := withLevel(term.Level16)
	if got := (Style{}).With(p).Frame("a\x1b[31mb"); got != "ab" {
		t.Errorf("空样式不包裹: %q", got)
	}
}
