package ctty

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var (
	watchOnce sync.Once
	exitMu    sync.Mutex
	exitDone  bool
	exitSig   os.Signal
	intMu     sync.Mutex
	intCh     = make(chan struct{})
)

func WatchSignals() {
	watchOnce.Do(func() {
		ch := make(chan os.Signal, 4)
		signal.Notify(ch, watchSignals()...)
		go dispatch(ch)
	})
}

func watchSignals() []os.Signal {
	out := make([]os.Signal, 0, len(exitSignals)+len(interruptSignals))
	out = append(out, exitSignals...)
	return append(out, interruptSignals...)
}

func dispatch(ch <-chan os.Signal) {
	seen := 0
	for sig := range ch {
		if !isExitSignal(sig) {
			broadcast()
			continue
		}
		seen++
		if seen > 1 {
			emergencyRestore()
			os.Exit(ExitStatus())
		}
		Exit(sig)
	}
}

func isExitSignal(sig os.Signal) bool {
	for _, s := range exitSignals {
		if s == sig {
			return true
		}
	}
	return false
}

func Exit(sig os.Signal) {
	exitMu.Lock()
	if !exitDone {
		exitDone = true
		exitSig = sig
	}
	exitMu.Unlock()
	broadcast()
}

func Exiting() bool {
	exitMu.Lock()
	defer exitMu.Unlock()
	return exitDone
}

func ExitSignal() os.Signal {
	exitMu.Lock()
	defer exitMu.Unlock()
	return exitSig
}

func ExitStatus() int {
	n, ok := ExitSignal().(syscall.Signal)
	if !ok {
		return 0
	}
	return 128 + int(n)
}

func Interrupted() <-chan struct{} {
	if Exiting() {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	intMu.Lock()
	defer intMu.Unlock()
	return intCh
}

func broadcast() {
	intMu.Lock()
	close(intCh)
	intCh = make(chan struct{})
	intMu.Unlock()
}
