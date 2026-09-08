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

type spinRender func(elapsed time.Duration, frame string) string

type spinner struct {
	mu      *sync.Mutex
	tty     bool
	out     io.Writer
	stopCh  chan struct{}
	stopped chan struct{}
	active  bool
}

func newSpinner(mu *sync.Mutex, tty bool) *spinner {
	return &spinner{mu: mu, tty: tty, out: os.Stdout}
}

func (s *spinner) start(render spinRender) {
	if !s.tty {
		return
	}
	s.stop()
	s.stopCh = make(chan struct{})
	s.stopped = make(chan struct{})
	s.active = true
	go s.loop(time.Now(), render)
}

func (s *spinner) loop(start time.Time, render spinRender) {
	defer close(s.stopped)
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	frame := 0
	for {
		s.mu.Lock()
		fmt.Fprint(s.out, style.ClearLineHome()+render(time.Since(start), spinnerFrames[frame%len(spinnerFrames)]))
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
