package repl

import (
	"time"

	"github.com/LaoQi/tanya/ctty"
)

// NotifyReason 标记通知原因：触发语义归 REPL（什么事件值得打扰用户），行为归 Notifier。
type NotifyReason uint8

const (
	NotifyTurnDone NotifyReason = iota + 1
	NotifyNeedInput
)

// Notification 是一次通知的载荷：Notifier 按 Reason 分派，只读自己需要的字段。
type Notification struct {
	Reason   NotifyReason
	Duration time.Duration
	Failed   bool
	Tool     string
}

type Notifier interface {
	Notify(Notification)
}

// bellNotifier 是当前唯一的通知行为：往控制终端写一声 BEL，载荷全忽略（终端只有这一种可发声行为）。
type bellNotifier struct{}

func (bellNotifier) Notify(Notification) { _ = ctty.Bell() }

func BellNotifier() Notifier { return bellNotifier{} }
