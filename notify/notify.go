// Package notify 实现通知行为：把成品载荷交给终端提示（bell/OSC）或外部程序。
// 载荷类型 Notification 也在此定义——agent/tools 等库侧只消费事件，不定义通知语义。
package notify

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/LaoQi/battlelab/tools/shell"
)

// Notification 是通知载荷的成品形态：已清洗为单行、已截断，由触发方（repl）生成、
// 行为方（终端提示音/OSC/外部程序）只读。
type Notification struct {
	Title   string
	Content string
	Kind    string
}

const (
	NotifyCmdTimeout  = 3 * time.Second
	notifyPlaceholder = "{title}"
)

var notifyPlaceholders = []string{"{title}", "{content}", "{kind}"}

// Command 按配置调用外部程序：异步、单飞（上一次未结束就丢弃本次）、固定超时、stdio 接空设备、失败静默。
type Command struct {
	argv    []string
	kind    shell.Kind
	tmpl    string
	timeout time.Duration
	run     func(context.Context, []string) error
	sem     chan struct{}
}

// NewCommand 校验模板（未知占位符、花括号不配对、占位符紧邻引号）后构造通知行为。
func NewCommand(inv shell.Invocation, tmpl string) (*Command, error) {
	if err := ValidateTemplate(tmpl); err != nil {
		return nil, err
	}
	return &Command{
		argv:    inv.Argv,
		kind:    inv.Kind,
		tmpl:    tmpl,
		timeout: NotifyCmdTimeout,
		run:     RunCommand,
		sem:     make(chan struct{}, 1),
	}, nil
}

func RunCommand(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run()
}

func (c *Command) Notify(n Notification) {
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
func (c *Command) Render(n Notification) string {
	q := func(s string) string { return Quote(c.kind, s) }
	return strings.NewReplacer(
		"{title}", q(n.Title),
		"{content}", q(n.Content),
		"{kind}", q(n.Kind),
	).Replace(c.tmpl)
}

func Quote(kind shell.Kind, s string) string {
	switch kind {
	case shell.KindPowerShell:
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	case shell.KindCmd:
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	default:
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
}

func ValidateTemplate(tmpl string) error {
	for i := 0; i < len(tmpl); i++ {
		switch tmpl[i] {
		case '{':
			j := strings.IndexByte(tmpl[i:], '}')
			if j < 0 {
				return fmt.Errorf(msgBraceFmt, tmpl[i:])
			}
			name := tmpl[i : i+j+1]
			if !isPlaceholder(name) {
				return fmt.Errorf(msgPlaceholderFmt, name)
			}
			if quotedPlaceholder(tmpl, i, i+j) {
				return fmt.Errorf(msgQuotedFmt, name)
			}
			i += j
		case '}':
			return fmt.Errorf(msgBraceFmt, tmpl[i:])
		}
	}
	return nil
}

const (
	msgPlaceholderFmt = "notify_cmd 含未知占位符 %s（可用: {title} {content} {kind}）"
	msgBraceFmt       = "notify_cmd 花括号不配对: %s"
	msgQuotedFmt      = "notify_cmd 的占位符 %s 紧邻引号：占位符自带引号，去掉外层引号"
)

func isPlaceholder(name string) bool {
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
