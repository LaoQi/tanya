package repl

import (
	"fmt"
	"github.com/LaoQi/tanyan/render/term"
	"github.com/LaoQi/tanyan/render/theme"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 100 * time.Millisecond

const spinnerStopTimeout = 200 * time.Millisecond

type spinKind uint8

const (
	spinWaiting spinKind = iota + 1
	spinThinking
	spinRunning
)

func spinLine(kind spinKind, sem theme.Semantics, elapsed time.Duration, frame string) string {
	switch kind {
	case spinThinking:
		return sem.Think.Sprint(fmt.Sprintf(SpinThinking, frame, spinElapsed(elapsed)))
	case spinRunning:
		return sem.Run.Sprint(fmt.Sprintf(SpinRunning, frame, spinElapsed(elapsed)))
	default:
		return sem.Warn.Sprint(fmt.Sprintf(SpinWaiting, frame, spinElapsed(elapsed)))
	}
}

type spinner struct {
	mu      sync.Mutex
	tty     bool
	sem     theme.Semantics
	out     *output
	stopCh  chan struct{}
	stopped chan struct{}
	active  bool
	kind    spinKind
}

func newSpinner(out *output, tty bool, sem theme.Semantics) *spinner {
	return &spinner{out: out, tty: tty, sem: sem}
}

func (s *spinner) setSemantics(sem theme.Semantics) {
	s.mu.Lock()
	s.sem = sem
	s.mu.Unlock()
}

func (s *spinner) start(kind spinKind) {
	if !s.tty {
		return
	}
	s.stop()
	s.stopCh = make(chan struct{})
	s.stopped = make(chan struct{})
	s.active = true
	s.kind = kind
	go s.loop(time.Now())
}

func (s *spinner) setKind(kind spinKind) {
	if !s.tty || !s.active {
		return
	}
	s.mu.Lock()
	s.kind = kind
	s.mu.Unlock()
}

func (s *spinner) loop(start time.Time) {
	defer close(s.stopped)
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	frame := 0
	for {
		s.mu.Lock()
		line := term.ClearLineHome() + spinLine(s.kind, s.sem, time.Since(start), spinnerFrames[frame%len(spinnerFrames)])
		s.mu.Unlock()
		s.out.emit(KindSpinner, line)
		frame++
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
		}
	}
}

func (s *spinner) stop() {
	if !s.active {
		return
	}
	close(s.stopCh)
	clean := false
	select {
	case <-s.stopped:
		clean = true
	case <-time.After(spinnerStopTimeout):
	}
	s.active = false
	if clean {
		s.out.emit(KindSpinner, term.ClearLineHome())
	}
}

func spinElapsed(d time.Duration) string {
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
