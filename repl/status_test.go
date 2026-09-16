package repl

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/render/term"
)

var sgrSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func assertNoCursorControl(t *testing.T, s string) {
	t.Helper()
	rest := sgrSeq.ReplaceAllString(s, "")
	if strings.ContainsRune(rest, '\r') || strings.ContainsRune(rest, 0x1b) {
		t.Errorf("不得含光标控制序列: %q", s)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

func TestStatusSeconds(t *testing.T) {
	cases := []struct {
		sec  int
		want string
	}{
		{0, "0s"}, {9, "9s"}, {59, "59s"}, {60, "1m00s"}, {70, "1m10s"},
		{600, "10m00s"}, {3600, "1h00m"}, {3660, "1h01m"},
	}
	for _, c := range cases {
		if got := statusSeconds(c.sec); got != c.want {
			t.Errorf("statusSeconds(%d) = %q, want %q", c.sec, got, c.want)
		}
	}
}

func TestStatusLineNoCursorControl(t *testing.T) {
	sem := testSem()
	for _, kind := range []statusKind{statusWaiting, statusToolRunning} {
		for _, sec := range []int{0, 1, 59, 70, 3660} {
			assertNoCursorControl(t, statusLine(kind, sem, sec))
		}
	}
	if got := term.Strip(statusLine(statusWaiting, sem, 0)); got != MsgStatusWaiting+" 0s " {
		t.Errorf("等待行 = %q, want %q", got, MsgStatusWaiting+" 0s ")
	}
	if got := term.Strip(statusLine(statusToolRunning, sem, 70)); got != MsgStatusRunning+" 1m10s " {
		t.Errorf("执行行 = %q, want %q", got, MsgStatusRunning+" 1m10s ")
	}
}

// stepClock 返回每次调用前进 step 的假时钟，用于把行首秒数固定成可断言的值。
func stepClock(step time.Duration) func() time.Time {
	base := time.Now()
	var n int
	return func() time.Time {
		n++
		return base.Add(time.Duration(n) * step)
	}
}

// TestHeartbeatLineStructure 断言行结构：每行行首是含起始秒数的固定前缀，行内只追加点，
// 满 span 个点即换行并以当时秒数开新行，末行未满点。
func TestHeartbeatLineStructure(t *testing.T) {
	var buf syncBuf
	h := newHeartbeat(newOutput(&buf, allVisible()), testSem())
	h.interval = 2 * time.Millisecond
	h.span = 3
	h.now = stepClock(3 * time.Second)
	h.start(statusWaiting, true)
	waitUntil(t, "跨过两行", func() bool { return strings.Count(buf.String(), "\n") >= 2 })
	h.stop()
	got := buf.String()
	assertNoCursorControl(t, got)
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("停止应补一个换行收尾当前行: %q", got)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("行数不足: %q", got)
	}
	for i, line := range lines {
		plain := term.Strip(line)
		head := MsgStatusWaiting + " " + statusSeconds(i*h.span) + " "
		if !strings.HasPrefix(plain, head) {
			t.Errorf("第 %d 行前缀 = %q, want %q", i, plain, head)
			continue
		}
		dots := strings.TrimPrefix(plain, head)
		if strings.Trim(dots, ".") != "" {
			t.Errorf("第 %d 行只能追加点: %q", i, plain)
			continue
		}
		if i < len(lines)-1 && len(dots) != h.span {
			t.Errorf("第 %d 行点数 = %d, want %d: %q", i, len(dots), h.span, plain)
		}
		if i == len(lines)-1 && len(dots) >= h.span {
			t.Errorf("末行满点即应换行: %q", plain)
		}
	}
}

// TestHeartbeatSecondsFromClock 锁定秒数取墙钟而非 span 计数：行首秒数按真实经过时间给出。
func TestHeartbeatSecondsFromClock(t *testing.T) {
	var buf syncBuf
	h := newHeartbeat(newOutput(&buf, allVisible()), testSem())
	h.interval = 2 * time.Millisecond
	h.span = 2
	h.now = stepClock(7 * time.Second)
	h.start(statusWaiting, true)
	waitUntil(t, "跨过两行", func() bool { return strings.Count(buf.String(), "\n") >= 2 })
	h.stop()
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	for i, want := range []string{"0s", "7s", "14s"} {
		if i >= len(lines) {
			t.Fatalf("行数不足: %q", buf.String())
		}
		if got := term.Strip(lines[i]); !strings.HasPrefix(got, MsgStatusWaiting+" "+want+" ") {
			t.Errorf("第 %d 行 = %q, want 前缀 %q", i, got, MsgStatusWaiting+" "+want)
		}
	}
}

// TestHeartbeatDotSharesLabelColor 锁住点与行首同色：两阶段各用各自语义色包裹单点，
// 且点自带 reset（行不留在着色态）。
func TestHeartbeatDotSharesLabelColor(t *testing.T) {
	ttyProfile(t, term.Profile{TTY: true, Colors: term.Level16})
	sem := testSem()
	for _, c := range []struct {
		kind statusKind
		text string
		dot  string
	}{
		{statusWaiting, sem.Warn.Sprint(MsgStatusWaiting + " 0s"), sem.Warn.Sprint(".")},
		{statusToolRunning, sem.Run.Sprint(MsgStatusRunning + " 0s"), sem.Run.Sprint(".")},
	} {
		var buf syncBuf
		h := newHeartbeat(newOutput(&buf, allVisible()), sem)
		h.interval = 2 * time.Millisecond
		h.start(c.kind, true)
		waitUntil(t, "出现点", func() bool { return strings.Contains(buf.String(), c.dot) })
		h.stop()
		got := buf.String()
		if !strings.Contains(got, c.text) {
			t.Errorf("行首应带语义色: %q", got)
		}
		if !strings.Contains(got, c.dot) {
			t.Errorf("点应带行首同色: %q", got)
		}
		if strings.HasSuffix(got, ".") {
			t.Errorf("点后应立即复位颜色: %q", got)
		}
	}
}

// TestHeartbeatSetPhase 锁住换相位语义：只在心跳在跑且相位确实变化时动作——收尾当前行、
// 以新前缀开新行（一次写完，不重绘）；秒数沿用同一时钟（不重置为 0），点数归零；同相位重复事件是 no-op。
func TestHeartbeatSetPhase(t *testing.T) {
	var buf syncBuf
	h := newHeartbeat(newOutput(&buf, allVisible()), testSem())
	h.interval = time.Hour
	h.now = stepClock(3 * time.Second)

	h.setPhase(statusThinking)
	if got := buf.String(); got != "" {
		t.Fatalf("未起心跳时换相位不应写字节: %q", got)
	}

	h.start(statusWaiting, true)
	if got := term.Strip(buf.String()); got != MsgStatusWaiting+" 0s " {
		t.Fatalf("起始行 = %q", got)
	}
	h.setPhase(statusThinking)
	want := MsgStatusWaiting + " 0s \n" + MsgStatusThinking + " 3s "
	if got := term.Strip(buf.String()); got != want {
		t.Errorf("换相位 = 收尾旧行 + 新前缀开新行且秒数延续\n got %q\nwant %q", got, want)
	}
	if got := statusColor(statusThinking, testSem()); got != testSem().Think {
		t.Errorf("思考相位应取 Think 语义色: %+v", got)
	}
	after := buf.String()
	h.setPhase(statusThinking)
	h.setPhase(statusThinking)
	if got := buf.String(); got != after {
		t.Errorf("同相位重复事件不应再开行: %q -> %q", after, got)
	}
	h.stop()
	stopped := buf.String()
	if !strings.HasSuffix(term.Strip(stopped), "\n") {
		t.Errorf("停止应收尾思考行: %q", stopped)
	}
	h.setPhase(statusWaiting)
	if got := buf.String(); got != stopped {
		t.Errorf("停止后换相位不应再开行: %q -> %q", stopped, got)
	}
}

func TestHeartbeatRestartStartsFreshLine(t *testing.T) {
	var buf syncBuf
	h := newHeartbeat(newOutput(&buf, allVisible()), testSem())
	h.interval = 2 * time.Millisecond
	h.start(statusWaiting, true)
	waitUntil(t, "出现点", func() bool { return strings.Contains(term.Strip(buf.String()), ".") })
	h.start(statusToolRunning, true)
	waitUntil(t, "执行行出现", func() bool {
		return strings.Contains(term.Strip(buf.String()), "\n"+MsgStatusRunning+" 0s")
	})
	h.stop()
	got := term.Strip(buf.String())
	assertNoCursorControl(t, buf.String())
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("停止应补换行: %q", got)
	}
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if !strings.HasPrefix(line, MsgStatusWaiting+" ") && !strings.HasPrefix(line, MsgStatusRunning+" ") {
			t.Errorf("换行后必须是行首前缀: %q", line)
		}
	}
}

func TestHeartbeatDisabledNoOutput(t *testing.T) {
	var buf syncBuf
	h := newHeartbeat(newOutput(&buf, allVisible()), testSem())
	h.interval = 5 * time.Millisecond
	h.start(statusWaiting, false)
	time.Sleep(20 * time.Millisecond)
	h.stop()
	if got := buf.String(); got != "" {
		t.Errorf("未启用时不应输出: %q", got)
	}
}

func TestHeartbeatStopsSilently(t *testing.T) {
	var buf syncBuf
	h := newHeartbeat(newOutput(&buf, allVisible()), testSem())
	h.interval = 5 * time.Millisecond
	h.start(statusWaiting, true)
	waitUntil(t, "出现点", func() bool { return strings.Contains(term.Strip(buf.String()), ".") })
	h.stop()
	first := buf.String()
	assertNoCursorControl(t, first)
	if !strings.HasSuffix(first, "\n") {
		t.Errorf("停止应收尾当前行: %q", first)
	}
	time.Sleep(20 * time.Millisecond)
	if got := buf.String(); got != first {
		t.Errorf("停止后不应再写字节: %q -> %q", first, got)
	}
}

func TestHeartbeatLiveRejectsRetiredLoop(t *testing.T) {
	h := newHeartbeat(newOutput(&syncBuf{}, allVisible()), testSem())
	oldCh, newCh := make(chan struct{}), make(chan struct{})
	h.active, h.stopCh = true, newCh
	if h.live(oldCh) {
		t.Error("stop 超时后重开：退役 loop 不得认领新代际")
	}
	if !h.live(newCh) {
		t.Error("当前代际应可继续 tick")
	}
	h.active = false
	if h.live(newCh) {
		t.Error("已停止的代际不应继续 tick")
	}
}

func TestHeartbeatStopIsIdempotent(t *testing.T) {
	var buf syncBuf
	h := newHeartbeat(newOutput(&buf, allVisible()), testSem())
	h.interval = 5 * time.Millisecond
	h.stop()
	if got := buf.String(); got != "" {
		t.Errorf("未起心跳时停止不应写字节: %q", got)
	}
	h.start(statusWaiting, true)
	waitUntil(t, "出现点", func() bool { return strings.Contains(term.Strip(buf.String()), ".") })
	h.stop()
	first := buf.String()
	h.stop()
	if got := buf.String(); got != first {
		t.Errorf("重复停止不应再写字节: %q -> %q", first, got)
	}
}
