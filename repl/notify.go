package repl

import (
	"fmt"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/ctty"
	"github.com/LaoQi/tanya/render/present"
	"github.com/LaoQi/tanya/render/term"
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
	Notify(present.Notification)
}

// notifySink 是通知在事件流上的旁路消费者：触发语义由事实驱动，不再由 turn 内的硬编码分支发起。
type notifySink struct{ r *REPL }

func (n notifySink) Emit(e agent.Event) {
	switch e.Kind {
	case agent.EventToolStart:
		if e.Interactive {
			n.r.notify(Notification{Reason: NotifyNeedInput, Tool: e.ToolName})
		}
	case agent.EventTurnEnd:
		if !e.Turn.Interrupted {
			n.r.notify(Notification{Reason: NotifyTurnDone, Duration: e.Turn.Duration, Failed: e.Turn.Failed})
		}
	}
}

const (
	notifyKindDone   = "done"
	notifyKindFailed = "failed"
	notifyKindInput  = "input"

	notifyTitleMax   = 40
	notifyContentMax = 200
)

// payloadOf 把 REPL 侧的触发语义成品化成交警载荷：清洗为单行、按上限截断。
func payloadOf(n Notification) (present.Notification, bool) {
	var t present.Notification
	switch n.Reason {
	case NotifyNeedInput:
		t = present.Notification{Content: fmt.Sprintf(MsgNotifyInputFmt, n.Tool), Kind: notifyKindInput}
	case NotifyTurnDone:
		if n.Failed {
			t = present.Notification{Content: fmt.Sprintf(MsgNotifyFailedFmt, turnDuration(n.Duration)), Kind: notifyKindFailed}
		} else {
			t = present.Notification{Content: fmt.Sprintf(MsgNotifyDoneFmt, turnDuration(n.Duration)), Kind: notifyKindDone}
		}
	default:
		return present.Notification{}, false
	}
	t.Title = clampLine(NotifyTitle, notifyTitleMax)
	t.Content = clampLine(t.Content, notifyContentMax)
	return t, true
}

func clampLine(s string, w int) string { return term.Truncate(term.OneLine(s), w) }

type notifiers []Notifier

func (ns notifiers) Notify(n present.Notification) {
	for _, x := range ns {
		x.Notify(n)
	}
}

// Notifiers 把若干行为合成一个 Notifier（fan-out，各自独立开关）；全为 nil 时返回 nil（门禁零开销）。
func Notifiers(list ...Notifier) Notifier {
	live := make([]Notifier, 0, len(list))
	for _, n := range list {
		if n != nil {
			live = append(live, n)
		}
	}
	switch len(live) {
	case 0:
		return nil
	case 1:
		return live[0]
	}
	return notifiers(live)
}

// oscNotifier 走终端原生 OSC 9 通知：尽力而为，终端不认就只是没有效果，失败静默。
type oscNotifier struct {
	write func(string) error
}

func (o oscNotifier) Notify(n present.Notification) {
	_ = o.write(n.Title + ": " + n.Content)
}

func OSCNotifier() Notifier { return oscNotifier{write: ctty.NotifyOSC} }

// bellNotifier 往控制终端写一声 BEL，载荷全忽略（终端只有这一种可发声行为）。
type bellNotifier struct{}

func (bellNotifier) Notify(present.Notification) { _ = ctty.Bell() }

func BellNotifier() Notifier { return bellNotifier{} }
