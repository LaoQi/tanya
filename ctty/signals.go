package ctty

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var (
	watchOnce    sync.Once
	watchNotify  sync.Once
	resizeNotify sync.Once
	watchCh      chan os.Signal
	resizeMu     sync.Mutex
	resizeSubs   []func()
	exitMu       sync.Mutex
	exitDone     bool
	exitSig      os.Signal
	intMu        sync.Mutex
	intCh        = make(chan struct{})
)

// WatchSignals 安装退出与中断信号监听（语义与历史一致：sync.Once、由 main 单点调用）。
func WatchSignals() {
	watchNotify.Do(func() {
		signal.Notify(signals(), watchSignals()...)
	})
}

// OnResize 订阅终端尺寸变更（SIGWINCH）。武装按信号类惰性：只登记 resize 清单进同一个
// 分发器，不牵动退出/中断监听——readline 作为库被嵌入时不应连带装上 SIGTERM/SIGHUP 处置。
// 无 resize 来源的平台（windows 与 stub）返回 no-op 取消函数、永不回调。
func OnResize(fn func()) func() {
	if len(resizeSignals) == 0 {
		return func() {}
	}
	resizeNotify.Do(func() {
		signal.Notify(signals(), resizeSignals...)
	})
	resizeMu.Lock()
	resizeSubs = append(resizeSubs, fn)
	idx := len(resizeSubs) - 1
	resizeMu.Unlock()
	return func() {
		resizeMu.Lock()
		defer resizeMu.Unlock()
		if idx < len(resizeSubs) {
			resizeSubs[idx] = nil
		}
	}
}

func signals() chan os.Signal {
	watchOnce.Do(func() {
		watchCh = make(chan os.Signal, 4)
		go dispatch(watchCh)
	})
	return watchCh
}

func notifyResize() {
	resizeMu.Lock()
	fns := make([]func(), 0, len(resizeSubs))
	for _, fn := range resizeSubs {
		if fn != nil {
			fns = append(fns, fn)
		}
	}
	resizeMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func watchSignals() []os.Signal {
	out := make([]os.Signal, 0, len(exitSignals)+len(interruptSignals))
	out = append(out, exitSignals...)
	return append(out, interruptSignals...)
}

func dispatch(ch <-chan os.Signal) {
	seen := 0
	for sig := range ch {
		if isResizeSignal(sig) {
			notifyResize()
			continue
		}
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

func isResizeSignal(sig os.Signal) bool {
	for _, s := range resizeSignals {
		if s == sig {
			return true
		}
	}
	return false
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
