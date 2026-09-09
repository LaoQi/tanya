package repl

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/LaoQi/tanyan/style"
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

func spinLine(kind spinKind, elapsed time.Duration, frame string) string {
	switch kind {
	case spinThinking:
		return style.Think.Sprint(fmt.Sprintf(SpinThinking, frame, spinElapsed(elapsed)))
	case spinRunning:
		return style.Run.Sprint(fmt.Sprintf(SpinRunning, frame, spinElapsed(elapsed)))
	default:
		return style.Warn.Sprint(fmt.Sprintf(SpinWaiting, frame, spinElapsed(elapsed)))
	}
}

type spinner struct {
	mu      *sync.Mutex
	tty     bool
	out     io.Writer
	stopCh  chan struct{}
	stopped chan struct{}
	active  bool
	kind    spinKind
}

func newSpinner(mu *sync.Mutex, tty bool) *spinner {
	return &spinner{mu: mu, tty: tty, out: os.Stdout}
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
		fmt.Fprint(s.out, style.ClearLineHome()+spinLine(s.kind, time.Since(start), spinnerFrames[frame%len(spinnerFrames)]))
		s.mu.Unlock()
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
		s.mu.Lock()
		fmt.Fprint(s.out, style.ClearLineHome())
		s.mu.Unlock()
	}
}

func spinElapsed(d time.Duration) string {
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
