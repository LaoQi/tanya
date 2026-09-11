package repl

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/style"
)

func ttyProfile(t *testing.T, p style.Profile) {
	t.Helper()
	old := style.GetProfile()
	style.SetProfile(p)
	t.Cleanup(func() { style.SetProfile(old) })
}

func TestTurnSepTimeOnly(t *testing.T) {
	ttyProfile(t, style.Profile{TTY: true, Colors: style.Level16, Unicode: true})
	out := turnSep(0)
	plain := style.Strip(out)
	if !regexp.MustCompile(`^\n──── \d{2}:\d{2}:\d{2}\n$`).MatchString(plain) {
		t.Errorf("回合分隔线格式不符: %q", plain)
	}
	if !strings.HasPrefix(out, "\n\x1b[32m") || !strings.HasSuffix(out, "\x1b[0m\n") {
		t.Errorf("回合分隔线应整体 Ok 包裹且有前导尾随换行: %q", out)
	}
	if strings.Contains(plain, "回合") {
		t.Errorf("无耗时时不应出现耗时字段: %q", plain)
	}
}

func TestTurnSepWithDuration(t *testing.T) {
	ttyProfile(t, style.Profile{TTY: true, Colors: style.Level16, Unicode: true})
	plain := style.Strip(turnSep(12*time.Second + 400*time.Millisecond))
	if !regexp.MustCompile(`^\n──── \d{2}:\d{2}:\d{2} · 回合 12\.4s\n$`).MatchString(plain) {
		t.Errorf("带耗时分隔线格式不符: %q", plain)
	}
}

func TestTurnSepNoColor(t *testing.T) {
	ttyProfile(t, style.Profile{TTY: true, Colors: style.LevelNone, Unicode: true})
	out := turnSep(time.Second)
	if strings.Contains(out, "\x1b[") {
		t.Errorf("无色环境不应出现 SGR: %q", out)
	}
	if !regexp.MustCompile(`^\n──── \d{2}:\d{2}:\d{2} · 回合 1\.0s\n$`).MatchString(out) {
		t.Errorf("无色环境文本格式不符: %q", out)
	}
}

func TestTurnSepNonTTYBypass(t *testing.T) {
	ttyProfile(t, style.Profile{TTY: false, Colors: style.LevelNone, Unicode: true})
	if out := turnSep(0); out != "" {
		t.Errorf("非 TTY 不应打印分隔线: %q", out)
	}
	if out := turnSep(3 * time.Second); out != "" {
		t.Errorf("非 TTY 不应打印带耗时分隔线: %q", out)
	}
}

func TestTurnDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0ms"},
		{900 * time.Millisecond, "900ms"},
		{time.Second, "1.0s"},
		{12*time.Second + 400*time.Millisecond, "12.4s"},
		{63 * time.Second, "1m03s"},
		{754 * time.Second, "12m34s"},
		{time.Hour + 2*time.Minute, "1h02m"},
	}
	for _, c := range cases {
		if got := turnDuration(c.in); got != c.want {
			t.Errorf("turnDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTurnSinkGapOnce(t *testing.T) {
	ttyProfile(t, style.Profile{TTY: true, Colors: style.Level16, Unicode: true})
	r, err := NewREPL(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	r.stream = func(agent.Event) { seen++ }
	sink := r.turnSink()
	out := captureStdout(func() {
		sink(agent.Event{Kind: agent.EventRequestStart})
		sink(agent.Event{Kind: agent.EventToolStart})
	})
	if out != "\n" {
		t.Errorf("首个事件前应恰好补一个空行: %q", out)
	}
	if seen != 2 {
		t.Errorf("事件应全部透传: %d", seen)
	}
}

func TestTurnSinkNonTTYNoGap(t *testing.T) {
	ttyProfile(t, style.Profile{TTY: false, Colors: style.LevelNone, Unicode: true})
	r, err := NewREPL(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	r.stream = func(agent.Event) { seen++ }
	sink := r.turnSink()
	out := captureStdout(func() {
		sink(agent.Event{Kind: agent.EventRequestStart})
		sink(agent.Event{Kind: agent.EventResponse})
	})
	if out != "" {
		t.Errorf("非 TTY 不应补空行: %q", out)
	}
	if seen != 2 {
		t.Errorf("事件应全部透传: %d", seen)
	}
}
