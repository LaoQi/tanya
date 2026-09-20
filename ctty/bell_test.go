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
