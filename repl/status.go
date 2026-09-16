package repl

import (
	"fmt"
	"sync"
	"time"

	"github.com/LaoQi/tanya/render/style"
	"github.com/LaoQi/tanya/render/theme"
)

const (
	statusTickInterval = time.Second
	statusLineSpan     = 10
	statusStopTimeout  = 200 * time.Millisecond
	statusDotGap       = " "
)

type statusKind uint8

const (
	statusWaiting statusKind = iota + 1
	statusThinking
	statusToolRunning
)

func statusSeconds(sec int) string {
	switch {
	case sec >= 3600:
		return fmt.Sprintf("%dh%02dm", sec/3600, sec/60%60)
	case sec >= 60:
		return fmt.Sprintf("%dm%02ds", sec/60, sec%60)
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

func statusColor(kind statusKind, sem theme.Semantics) style.Style {
	switch kind {
	case statusThinking:
		return sem.Think
	case statusToolRunning:
		return sem.Run
	default:
		return sem.Warn
	}
}

func statusLabel(kind statusKind) string {
	switch kind {
	case statusThinking:
		return MsgStatusThinking
	case statusToolRunning:
		return MsgStatusRunning
	default:
		return MsgStatusWaiting
	}
}

func statusLine(kind statusKind, sem theme.Semantics, sec int) string {
	return statusColor(kind, sem).Sprint(statusLabel(kind)+" "+statusSeconds(sec)) + statusDotGap
}

func statusDot(kind statusKind, sem theme.Semantics) string {
	return statusColor(kind, sem).Sprint(".")
}

// heartbeat 是追加式心跳：满 span 个点换一行，行首写一次前缀（含开行时的真实等待秒数，之后不变），
// 行内每 tick 只追加一个点。不重绘、不清行；停止只补一个换行收尾当前行。
type heartbeat struct {
	mu       sync.Mutex
	out      *output
	sem      theme.Semantics
	interval time.Duration
	span     int
	now      func() time.Time

	stopCh  chan struct{}
	stopped chan struct{}
	active  bool
	open    bool
	kind    statusKind
	started time.Time
	dots    int
}

func newHeartbeat(out *output, sem theme.Semantics) *heartbeat {
	return &heartbeat{out: out, sem: sem, interval: statusTickInterval, span: statusLineSpan, now: time.Now}
}

func (h *heartbeat) setSemantics(sem theme.Semantics) {
	h.mu.Lock()
	h.sem = sem
	h.mu.Unlock()
}

func (h *heartbeat) start(kind statusKind, enabled bool) {
	if !enabled {
		return
	}
	h.stop()
	stopCh := make(chan struct{})
	stopped := make(chan struct{})
	interval := h.interval
	h.mu.Lock()
	h.kind = kind
	h.started = h.now()
	h.dots = 0
	h.open = true
	h.stopCh = stopCh
	h.stopped = stopped
	h.active = true
	line := statusLine(kind, h.sem, 0)
	h.mu.Unlock()
	h.out.emit(KindStatus, line)
	go h.loop(interval, stopCh, stopped)
}

func (h *heartbeat) loop(interval time.Duration, stopCh, stopped chan struct{}) {
	defer close(stopped)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			h.mu.Lock()
			if !h.live(stopCh) {
				h.mu.Unlock()
				return
			}
			chunk := h.tick()
			h.mu.Unlock()
			h.out.emit(KindStatus, chunk)
		}
	}
}

// setPhase 只换一个正在跑的相位：同相位事件（思维链逐 token 到达）与未起心跳时都不写字节，
// 换相位则收尾当前行、以新前缀开新行，秒数沿用同一时钟（started 不改）。
func (h *heartbeat) setPhase(kind statusKind) {
	h.mu.Lock()
	if !h.active || h.kind == kind {
		h.mu.Unlock()
		return
	}
	h.kind = kind
	h.dots = 0
	line := statusLine(kind, h.sem, h.elapsed())
	h.mu.Unlock()
	h.out.emit(KindStatus, "\n"+line)
}

func (h *heartbeat) live(stopCh chan struct{}) bool {
	return h.active && h.stopCh == stopCh
}

func (h *heartbeat) tick() string {
	h.dots++
	dot := statusDot(h.kind, h.sem)
	if h.dots < h.span {
		return dot
	}
	h.dots = 0
	return dot + "\n" + statusLine(h.kind, h.sem, h.elapsed())
}

func (h *heartbeat) elapsed() int {
	now := h.now
	if now == nil {
		now = time.Now
	}
	return int(now().Sub(h.started) / time.Second)
}

func (h *heartbeat) stop() {
	h.mu.Lock()
	if !h.active {
		h.mu.Unlock()
		return
	}
	h.active = false
	open := h.open
	h.open = false
	stopCh, stopped := h.stopCh, h.stopped
	h.mu.Unlock()
	close(stopCh)
	select {
	case <-stopped:
	case <-time.After(statusStopTimeout):
	}
	if open {
		h.out.emit(KindStatus, "\n")
	}
}
