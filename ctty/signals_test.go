package ctty

import (
	"os"
	"testing"
	"time"
)

func resetSignals() {
	exitMu.Lock()
	exitDone, exitSig = false, nil
	exitMu.Unlock()
	intMu.Lock()
	intCh = make(chan struct{})
	intMu.Unlock()
}

func withResetSignals(t *testing.T) {
	t.Helper()
	resetSignals()
	t.Cleanup(resetSignals)
}

func TestExitStatusWithoutRequest(t *testing.T) {
	withResetSignals(t)
	if Exiting() || ExitSignal() != nil || ExitStatus() != 0 {
		t.Fatal("初始不应处于退出态")
	}
}

func TestExitStatusIgnoresNonSignal(t *testing.T) {
	withResetSignals(t)
	Exit(nil)
	if !Exiting() {
		t.Error("Exit(nil) 也应置退出态")
	}
	if ExitSignal() != nil {
		t.Errorf("ExitSignal: %v", ExitSignal())
	}
	if ExitStatus() != 0 {
		t.Errorf("非信号来源退出码应为 0: %d", ExitStatus())
	}
}

func TestInterruptedBroadcast(t *testing.T) {
	withResetSignals(t)
	ch := Interrupted()
	select {
	case <-ch:
		t.Fatal("初始不应关闭")
	default:
	}
	broadcast()
	select {
	case <-ch:
	default:
		t.Fatal("广播后应关闭")
	}
	if next := Interrupted(); next == ch {
		t.Error("广播后应换新通道")
	} else {
		select {
		case <-next:
			t.Error("新通道不应已关闭")
		default:
		}
	}
}

func TestInterruptedReflectsExit(t *testing.T) {
	withResetSignals(t)
	Exit(nil)
	select {
	case <-Interrupted():
	default:
		t.Fatal("退出态下新订阅者应拿到已关闭通道")
	}
}

func TestDispatchInterruptSignal(t *testing.T) {
	withResetSignals(t)
	ch := make(chan os.Signal, 2)
	go dispatch(ch)
	t.Cleanup(func() { close(ch) })
	before := Interrupted()
	ch <- os.Interrupt
	select {
	case <-before:
	case <-time.After(time.Second):
		t.Fatal("中断信号应广播")
	}
	if Exiting() {
		t.Error("中断信号不应置退出态")
	}
	if ExitStatus() != 0 {
		t.Errorf("ExitStatus: %d", ExitStatus())
	}
}
