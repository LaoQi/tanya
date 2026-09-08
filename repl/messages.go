package repl

const (
	MsgBye          = "再见"
	MsgErrLineFmt   = "错误: %v"
	MsgAskUsage     = "用法: tanyan ask \"问题\"\n"
	MsgUnknownCmd   = "未知命令，输入 /help 查看\n"
	MsgNoSessions   = "(无历史会话)\n"
	MsgNoHistoryMsg = "(当前会话无消息)\n"
	MsgCancelled    = "已取消\n"
)

const (
	MsgNewSession  = "已开启新会话\n"
	MsgLoadedSess  = "已载入会话 %s\n"
	MsgCurModel    = "当前模型: %s\n"
	MsgModelsFail  = "获取可用模型失败: %v\n"
	MsgModelsEmpty = "(接口未返回可用模型)\n"
	MsgModelsHead  = "可用模型:\n"
	MsgMarkCurrent = "* "
	MsgMarkPlain   = "  "

	MsgThinkUnset = "思考等级: 未设置\n"
	MsgCurEffort  = "思考等级: %s\n"
	MsgEffortSet  = "思考等级已设为 %s\n"
	MsgEffortOff  = "思考等级已关闭\n"
)

const (
	MsgInvalidIndex = "序号无效（1-%d，或 all）\n"
	MsgTotalMsgs    = "共 %d 条消息\n"
	MsgCallLabel    = "[调用 %s]"
	MsgLegacyHint   = "提示: 旧版本会话已冻结历史环境信息；建议 /new 开启新会话\n"
)

const (
	PickTitle        = "选择会话（↑/↓ 移动，Enter 确认，q 取消）:\r\n"
	PickNumTitle     = "输入序号选择会话（回车取消）:\n"
	PickNumPrompt    = "序号: "
	SessRow          = "%s%s  %s  %3d条  %s"
	PickCompleteItem = "/load %s  %s  %3d条  %s"
)

const (
	MsgLinesTotal = "共 %d 行"
	MsgLines      = "%d 行"
	MsgCachePct   = "缓存 %.2f%%"
	MsgCtxTokens  = "上下文 ~%s"
	MsgTruncNote  = "…中间省略 %d 字节…"
	MsgInterrupt  = "已中断"
	MsgTimeout    = "执行超时"
	MsgToolErr    = "错误: %s"
)

const (
	SpinWaiting = "%s 等待响应 %s"
	SpinRunning = "  %s 执行中 %s"
)

const helpText = `斜杠命令：
  /help            显示帮助
  /new             开启新会话（当前会话自动保存）
  /sessions        列出历史会话
  /load <id>       载入历史会话
  /context         显示上下文占用
  /history [n|all] 无参截断列表；n 全量查看单条；all 全量显示
  /model [name]    无参显示当前模型；带名切换模型
  /think [level]   无参显示思考等级；设置 minimal/low/medium/high/max，off 关闭
  /exit            退出
直接输入文本与 AI 对话；shell 工具直接执行，无需确认。
`

const welcomText = `
██████ ▄████▄ ███  ██ ██  ██ ▄████▄ 
  ██   ██▄▄██ ██ ▀▄██  ▀██▀  ██▄▄██ 
  ██   ██  ██ ██   ██   ██   ██  ██ 

输入 /help 查看命令

`

const (
	FlagConfig = "配置文件路径（默认 ~/.config/tanyan/config.yaml）"
	FlagMode   = "会话存储模式 local/global/auto（默认 auto）"
)
