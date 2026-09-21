package ctty

import "testing"

// TestBell 锁住"尽力而为"契约：有控制终端时写入 BEL 并返回 nil，没有时返回错误（不 panic）。
// 有控制终端的环境下该断言会响一声，属预期副作用。
func TestBell(t *testing.T) {
	tty, err := Open()
	if err == nil {
		tty.Close()
		if err := Bell(); err != nil {
			t.Errorf("有控制终端时 Bell 应成功: %v", err)
		}
		return
	}
	if err := Bell(); err == nil {
		t.Error("无控制终端时 Bell 应返回错误")
	}
}

func TestOSCFrame(t *testing.T) {
	if got, want := oscFrame("tanya: 回合结束 · 3.2s"), "\x1b]9;tanya: 回合结束 · 3.2s\a"; got != want {
		t.Errorf("OSC 帧不符: %q", got)
	}
}

// 与 Bell 同一契约：无控制终端返回错误、不 panic，也绝不写 stdout。
func TestNotifyOSCNoTTY(t *testing.T) {
	tty, err := Open()
	if err == nil {
		tty.Close()
		if err := NotifyOSC("tanya: 回合结束"); err != nil {
			t.Errorf("有控制终端时 NotifyOSC 应成功: %v", err)
		}
		return
	}
	if err := NotifyOSC("tanya: 回合结束"); err == nil {
		t.Error("无控制终端时 NotifyOSC 应返回错误")
	}
}
