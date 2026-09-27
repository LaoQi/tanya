package shell

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/LaoQi/tanya/render/present"
)

const (
	NotifyCmdTimeout  = 3 * time.Second
	notifyPlaceholder = "{title}"
)

var notifyPlaceholders = []string{"{title}", "{content}", "{kind}"}

// CommandNotifier 按配置调用外部程序：异步、单飞（上一次未结束就丢弃本次）、固定超时、stdio 接空设备、失败静默。
type CommandNotifier struct {
	argv    []string
	kind    Kind
	tmpl    string
	timeout time.Duration
	run     func(context.Context, []string) error
	sem     chan struct{}
}

// NewCommandNotifier 校验模板（未知占位符、花括号不配对、占位符紧邻引号）后构造通知行为。
func NewCommandNotifier(inv Invocation, tmpl string) (*CommandNotifier, error) {
	if err := ValidateNotifyTemplate(tmpl); err != nil {
		return nil, err
	}
	return &CommandNotifier{
		argv:    inv.Argv,
		kind:    inv.Kind,
		tmpl:    tmpl,
		timeout: NotifyCmdTimeout,
		run:     RunNotifyCommand,
		sem:     make(chan struct{}, 1),
	}, nil
}

func RunNotifyCommand(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run()
}

func (c *CommandNotifier) Notify(n present.Notification) {
	select {
	case c.sem <- struct{}{}:
	default:
		return
	}
	argv := append(append(make([]string, 0, len(c.argv)+1), c.argv...), c.Render(n))
	go func() {
		defer func() { <-c.sem }()
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		defer cancel()
		_ = c.run(ctx, argv)
	}()
}

// Render 单趟替换占位符：替换值按目标 shell 的引号规则包成字面量（值自带引号，配置里不必也不该再加）。
func (c *CommandNotifier) Render(n present.Notification) string {
	q := func(s string) string { return Quote(c.kind, s) }
	return strings.NewReplacer(
		"{title}", q(n.Title),
		"{content}", q(n.Content),
		"{kind}", q(n.Kind),
	).Replace(c.tmpl)
}

func Quote(kind Kind, s string) string {
	switch kind {
	case KindPowerShell:
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	case KindCmd:
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	default:
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
}

func ValidateNotifyTemplate(tmpl string) error {
	for i := 0; i < len(tmpl); i++ {
		switch tmpl[i] {
		case '{':
			j := strings.IndexByte(tmpl[i:], '}')
			if j < 0 {
				return fmt.Errorf(msgNotifyBraceFmt, tmpl[i:])
			}
			name := tmpl[i : i+j+1]
			if !isNotifyPlaceholder(name) {
				return fmt.Errorf(msgNotifyPlaceholderFmt, name)
			}
			if quotedPlaceholder(tmpl, i, i+j) {
				return fmt.Errorf(msgNotifyQuotedFmt, name)
			}
			i += j
		case '}':
			return fmt.Errorf(msgNotifyBraceFmt, tmpl[i:])
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
