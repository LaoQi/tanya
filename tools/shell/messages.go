package shell

import (
	"errors"

	"github.com/LaoQi/tanya/agent"
)

const (
	msgNotifyPlaceholderFmt = "notify_cmd 含未知占位符 %s（可用: {title} {content} {kind}）"
	msgNotifyBraceFmt       = "notify_cmd 花括号不配对: %s"
	msgNotifyQuotedFmt      = "notify_cmd 的占位符 %s 紧邻引号：占位符自带引号，去掉外层引号"
)

var ErrNoLend = errors.New(agent.MsgErrPrefix + "interactive 不支持（无法借出终端）")

const (
	MsgBadCwd              = agent.MsgErrPrefix + "cwd 不存在或不是目录: %s"
	MsgInterruptNotStarted = agent.MsgErrPrefix + "已中断（命令未执行）"
	MsgInterruptRunning    = agent.MsgErrPrefix + "已中断（进程已终止，输出可能不完整）"
	MsgTimedOut            = agent.MsgErrPrefix + "执行超时"
	MsgErrLine             = agent.MsgErrPrefix + "%s"
	MsgNoShellFmt          = "未找到可用 shell（已尝试 %s），请安装或在配置中指定 shell:"
	MsgShellOverrideFmt    = "配置的 shell %q 不可执行，请检查 shell: 或 TANYA_SHELL"
	MsgNoOutput            = "(无输出，退出码 0)"
	MsgTruncMiddle         = "[%s 中间截断 %d 字节]"
	MsgTruncTail           = "[%s 截断 %d 字节]"
)
