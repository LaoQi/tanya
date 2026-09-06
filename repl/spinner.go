package repl

import (
	"fmt"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 100 * time.Millisecond

type spinRender func(elapsed time.Duration, frame string) string

type spinner struct {
	mu      *sync.Mutex
	tty     bool
	stopCh  chan struct{}
	stopped chan struct{}
	active  bool
}

func newSpinner(mu *sync.Mutex, tty bool) *spinner {
	return &spinner{mu: mu, tty: tty}
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
		fmt.Print("\r\x1b[K" + render(time.Since(start), spinnerFrames[frame%len(spinnerFrames)]))
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
	<-s.stopped
	s.active = false
	s.mu.Lock()
	fmt.Print("\r\x1b[K")
	s.mu.Unlock()
}

func spinElapsed(d time.Duration) string {
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
