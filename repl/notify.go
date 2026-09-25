package repl

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/LaoQi/tanya/ctty"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/tools/shell"
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

const (
	notifyKindDone   = "done"
	notifyKindFailed = "failed"
	notifyKindInput  = "input"

	notifyTitleMax   = 40
	notifyContentMax = 200
	notifyCmdTimeout = 3 * time.Second
)

// notifyText 是通知文案的成品形态：已清洗为单行、已截断，标题与类别固定，内容由本包文案模板生成。
type notifyText struct {
	title   string
	content string
	kind    string
}

func payloadOf(n Notification) (notifyText, bool) {
	var t notifyText
	switch n.Reason {
	case NotifyNeedInput:
		t = notifyText{content: fmt.Sprintf(MsgNotifyInputFmt, n.Tool), kind: notifyKindInput}
	case NotifyTurnDone:
		if n.Failed {
			t = notifyText{content: fmt.Sprintf(MsgNotifyFailedFmt, turnDuration(n.Duration)), kind: notifyKindFailed}
		} else {
			t = notifyText{content: fmt.Sprintf(MsgNotifyDoneFmt, turnDuration(n.Duration)), kind: notifyKindDone}
		}
	default:
		return notifyText{}, false
	}
	t.title = NotifyTitle
	t.title = clampLine(t.title, notifyTitleMax)
	t.content = clampLine(t.content, notifyContentMax)
	return t, true
}

func clampLine(s string, w int) string { return term.Truncate(term.OneLine(s), w) }

type notifiers []Notifier

func (ns notifiers) Notify(n Notification) {
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

func (o oscNotifier) Notify(n Notification) {
	t, ok := payloadOf(n)
	if !ok {
		return
	}
	_ = o.write(t.title + ": " + t.content)
}

func OSCNotifier() Notifier { return oscNotifier{write: ctty.NotifyOSC} }

// commandNotifier 按配置调用外部程序：异步、单飞（上一次未结束就丢弃本次）、固定超时、stdio 接空设备、失败静默。
type commandNotifier struct {
	argv    []string
	kind    shell.Kind
	tmpl    string
	timeout time.Duration
	run     func(context.Context, []string) error
	sem     chan struct{}
}

// NewCommandNotifier 校验模板（未知占位符、花括号不配对、占位符紧邻引号）后构造通知行为。
func NewCommandNotifier(inv shell.Invocation, tmpl string) (Notifier, error) {
	if err := validateNotifyTemplate(tmpl); err != nil {
		return nil, err
	}
	return &commandNotifier{
		argv:    inv.Argv,
		kind:    inv.Kind,
		tmpl:    tmpl,
		timeout: notifyCmdTimeout,
		run:     runNotifyCommand,
		sem:     make(chan struct{}, 1),
	}, nil
}

func runNotifyCommand(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run()
}

func (c *commandNotifier) Notify(n Notification) {
	t, ok := payloadOf(n)
	if !ok {
		return
	}
	select {
	case c.sem <- struct{}{}:
	default:
		return
	}
	argv := append(append(make([]string, 0, len(c.argv)+1), c.argv...), c.render(t))
	go func() {
		defer func() { <-c.sem }()
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		defer cancel()
		_ = c.run(ctx, argv)
	}()
}

// render 单趟替换占位符：替换值按目标 shell 的引号规则包成字面量（值自带引号，配置里不必也不该再加）。
func (c *commandNotifier) render(t notifyText) string {
	q := func(s string) string { return shellQuote(c.kind, s) }
	return strings.NewReplacer(
		"{title}", q(t.title),
		"{content}", q(t.content),
		"{kind}", q(t.kind),
	).Replace(c.tmpl)
}

func shellQuote(kind shell.Kind, s string) string {
	switch kind {
	case shell.KindPowerShell:
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	case shell.KindCmd:
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	default:
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
}

var notifyPlaceholders = []string{"{title}", "{content}", "{kind}"}

func validateNotifyTemplate(tmpl string) error {
	for i := 0; i < len(tmpl); i++ {
		switch tmpl[i] {
		case '{':
			j := strings.IndexByte(tmpl[i:], '}')
			if j < 0 {
				return fmt.Errorf(MsgNotifyBraceFmt, tmpl[i:])
			}
			name := tmpl[i : i+j+1]
			if !isNotifyPlaceholder(name) {
				return fmt.Errorf(MsgNotifyPlaceholderFmt, name)
			}
			if quotedPlaceholder(tmpl, i, i+j) {
				return fmt.Errorf(MsgNotifyQuotedFmt, name)
			}
			i += j
		case '}':
			return fmt.Errorf(MsgNotifyBraceFmt, tmpl[i:])
		}
	}
	return nil
}

func isNotifyPlaceholder(name string) bool {
	for _, p := range notifyPlaceholders {
		if p == name {
			return true
		}
	}
	return false
}

func quotedPlaceholder(tmpl string, start, end int) bool {
	if start > 0 && isQuoteByte(tmpl[start-1]) {
		return true
	}
	return end+1 < len(tmpl) && isQuoteByte(tmpl[end+1])
}

func isQuoteByte(c byte) bool { return c == '"' || c == '\'' }

// bellNotifier 往控制终端写一声 BEL，载荷全忽略（终端只有这一种可发声行为）。
type bellNotifier struct{}

func (bellNotifier) Notify(Notification) { _ = ctty.Bell() }

func BellNotifier() Notifier { return bellNotifier{} }
