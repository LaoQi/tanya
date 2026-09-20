package agent

import "errors"

const (
	MsgErrPrefix           = "error: "
	MsgErrLine             = MsgErrPrefix + "%s"
	MsgParseArgs           = MsgErrPrefix + "参数解析失败: %v"
	MsgUnknownTool         = MsgErrPrefix + "未知工具 %s"
	MsgBadCwd              = MsgErrPrefix + "cwd 不存在或不是目录: %s"
	MsgInterruptNotStarted = MsgErrPrefix + "已中断（命令未执行）"
	MsgInterruptRunning    = MsgErrPrefix + "已中断（进程已终止，输出可能不完整）"
	MsgTimedOut            = MsgErrPrefix + "执行超时"
	MsgStopped             = MsgErrPrefix + "进程被终端挂起(Ctrl+Z)已终止"
)

const (
	MsgInterruptNotice = "[用户已中断本轮请求]"
	MsgErrorNoticeFmt  = "[本轮因错误中止：%s]"
	MsgInterruptedBare = "已中断"
)

const (
	MsgAPIKey      = "未配置 api_key（请写入配置文件或设置环境变量 TANYA_API_KEY）"
	MsgAPIStatus   = "API 错误 %d: %s"
	MsgModelList   = "解析模型列表失败: %w"
	MsgReadStream  = "读取流失败: %w"
	MsgConfigParse = "配置文件解析失败 %s: %w"
	MsgRespFailed  = "API 响应失败: %s"
	MsgRespHint404 = "端点可能不支持 responses 协议，可在配置中设置 api_protocol: chat"
)

const (
	MsgArchiveNoSave       = "不落盘模式（-n）下不能归档会话"
	MsgArchiveSkipIdle     = "近 5 分钟内修改"
	MsgArchiveSkipArchived = "已在归档卷内"
	MsgArchiveVolFailFmt   = "写入归档卷失败（%s）: %w"
	MsgArchiveReadOnly     = "归档只读会话不能继续对话（用 /fork 开新会话）"
	MsgControlStatArchive  = "会话: 归档只读 %s（未写入）"
	MsgBadSessionID        = "非法会话 id"
	MsgSessionGone         = "会话不存在: %s"
	MsgNoShellFmt          = "未找到可用 shell（已尝试 %s），请安装或在配置中指定 shell:"
	MsgShellOverrideFmt    = "配置的 shell %q 不可执行，请检查 shell: 或 TANYA_SHELL"
	MsgBadEffort           = "无效思考等级 %q（可选: minimal/low/medium/high/max/off）"
	MsgBadApiProtocol      = "无效 api_protocol %q（可选: chat/responses）"
	MsgBadArchiveThreshold = "无效 auto_archive_threshold %d（需 ≥ 2）"
	MsgBadArchiveKeep      = "无效 auto_archive_keep %d（需 ≥ 0 且小于 auto_archive_threshold %d）"
)

const (
	MsgInitFailFmt      = "初始化失败 %s: %w"
	MsgInitNotDir       = "已存在但不是目录"
	MsgInitNotFile      = "已存在但不是普通文件"
	MsgInitNoteSessions = "会话与历史"
	MsgInitNoteIgnore   = "忽略 .tanya/ 全部内容"
	MsgInitNoteAgents   = "项目说明骨架"
	MsgInitSkipNoTTY    = "非交互未询问；如需：echo '*' > .tanya/.gitignore"
	MsgInitSkipDeclined = "已选择不创建；如需：echo '*' > .tanya/.gitignore"
)

const (
	MsgEmptyModel            = "model 不能为空"
	MsgControlBadAction      = "未知 action %q（可选: get/set）"
	MsgControlUnset          = "(未设置)"
	MsgControlModel          = "model: %s"
	MsgControlEffort         = "reasoning_effort: %s"
	MsgControlUsageContext   = "上下文: %d tokens（最近一次请求）"
	MsgControlUsageHit       = "缓存命中: %d（%s）"
	MsgControlContextUnknown = "上下文: 未知（本轮尚无请求）"
	MsgControlStatSession    = "会话: %s"
	MsgControlStatNoSave     = "(不落盘)"
	MsgControlMessages       = "消息数: %d"
	MsgControlTotals         = "累计: prompt %d / completion %d / total %d"
	MsgControlModelSwitch    = "model: %s → %s"
	MsgControlEffortSwitch   = "reasoning_effort: %s → %s"
	MsgControlCacheHint      = "提示: 模型已切换，下一次请求生效；跨模型不复用 prompt cache"
	MsgControlModelsHead     = "可用模型（%d）:"
	MsgControlModelsEmpty    = "可用模型: 无"
	MsgControlModelsTrim     = "（仅列出前 %d 项，共 %d 项）"
	MsgControlSessionsHead   = "会话（%d，最新在前）:"
	MsgControlSessionsEmpty  = "会话: 无"
	MsgControlSessionsLine   = "  %s  %d 条  %s  %s"
	MsgControlSessionsTrim   = "（共 %d 个，仅列前 %d 个）"
	MsgControlMissingKey     = "缺少 key（可用: %s）"
	MsgControlBadKey         = "未知 key %q（可用: %s）"
	MsgControlReadOnlyKey    = "%q 是只读 key（可写: %s）"
	MsgControlNeedValue      = "set 需要 value（%s）"
	MsgControlConfigPath     = "配置文件: %s\n改动需重启 tanya 生效（本次会话可用 agent_custom 调整 model/reasoning_effort）"
	MsgControlModelSpec      = "非空模型名"
	MsgControlEffortSpec     = "minimal/low/medium/high/max/off，off 表示清空"
)

const (
	MsgNoOutput    = "(无输出，退出码 0)"
	MsgTruncMiddle = "[%s 中间截断 %d 字节]"
	MsgTruncTail   = "[%s 截断 %d 字节]"
)

const (
	MsgEnvDenied = "%s: <拒绝：疑似敏感变量>"
	MsgEnvUnset  = "%s: <未设置>"
)

const (
	MsgCalcUnparsed      = "表达式存在无法解析的部分: %q"
	MsgCalcDivZero       = "除数为零"
	MsgCalcUnexpectedEnd = "表达式意外结束"
	MsgCalcMissingRParen = "缺少右括号"
	MsgCalcWantNumber    = "第 %d 个字符处应为数字"
)

var (
	ErrArchiveReadOnly = errors.New(MsgArchiveReadOnly)
)
