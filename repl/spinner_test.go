package repl

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LaoQi/tanyan/style"
)

func TestSpinnerNonTTYNoop(t *testing.T) {
	var mu sync.Mutex
	sp := newSpinner(&mu, false)
	sp.start(spinWaiting)
	time.Sleep(250 * time.Millisecond)
	sp.stop()
	if sp.active {
		t.Error("非 TTY 下 spinner 不应激活")
	}
}

func TestSpinElapsed(t *testing.T) {
	if got := spinElapsed(2500 * time.Millisecond); got != "2s" {
		t.Errorf("got %q", got)
	}
	if got := spinElapsed(900 * time.Millisecond); got != "0s" {
		t.Errorf("got %q", got)
	}
}

type blockedWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (w *blockedWriter) Write(p []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	return len(p), nil
}

func TestSpinnerStopTimeout(t *testing.T) {
	var mu sync.Mutex
	sp := newSpinner(&mu, true)
	bw := &blockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	sp.out = bw
	sp.start(spinWaiting)
	<-bw.entered
	done := make(chan struct{})
	go func() {
		sp.stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop 应在有界超时后返回而不是永久阻塞")
	}
	if sp.active {
		t.Error("超时后 active 应置 false")
	}
	close(bw.release)
	select {
	case <-sp.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("spinner goroutine 未退出（遗留 goroutine 会与后续测试的全局状态写构成竞态）")
	}
}

func TestSpinLine(t *testing.T) {
	if got := spinLine(spinWaiting, 2*time.Second, "⠋"); !strings.Contains(got, "等待响应") || !strings.Contains(got, "2s") {
		t.Errorf("等待文案: %q", got)
	}
	if got := spinLine(spinThinking, 3*time.Second, "⠙"); !strings.Contains(got, "思考中") || !strings.Contains(got, "3s") {
		t.Errorf("思考文案: %q", got)
	}
	if got := spinLine(spinRunning, time.Second, "⠹"); !strings.Contains(got, "执行中") {
		t.Errorf("执行文案: %q", got)
	}
}

func TestSpinLineStateColors(t *testing.T) {
	old := style.GetProfile()
	style.SetProfile(style.Profile{TTY: true, Colors: style.Level16, Unicode: true})
	defer style.SetProfile(old)
	prev := style.CurrentSchemeName()
	style.ApplyScheme("default")
	style.ApplyPalette(nil)
	defer func() {
		style.ApplyScheme(prev)
		style.ApplyPalette(nil)
	}()

	sgr := func(st style.Style) string {
		out := st.Sprint("x")
		return out[:strings.Index(out, "m")+1]
	}
	colors := map[spinKind]string{}
	for _, k := range []spinKind{spinWaiting, spinThinking, spinRunning} {
		line := spinLine(k, time.Second, "⠋")
		i := strings.Index(line, "\x1b[")
		if i < 0 {
			t.Fatalf("%d 缺少颜色码: %q", k, line)
		}
		j := strings.Index(line[i:], "m")
		colors[k] = line[i : i+j+1]
	}
	if colors[spinWaiting] == colors[spinThinking] || colors[spinThinking] == colors[spinRunning] || colors[spinWaiting] == colors[spinRunning] {
		t.Errorf("三态颜色应互不相同: %v", colors)
	}
	want := map[spinKind]string{
		spinWaiting:  sgr(style.Warn),
		spinThinking: sgr(style.Think),
		spinRunning:  sgr(style.Run),
	}
	for k, code := range want {
		if colors[k] != code {
			t.Errorf("%d 应为对应语义色 %q: %q", k, code, colors[k])
		}
	}
}
