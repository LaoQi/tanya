package repl

const (
	MsgBye          = "再见"
	MsgErrLineFmt   = "错误: %v"
	MsgAskUsage     = "用法: tanyan ask \"问题\"\n"
	MsgNoSessions   = "(无历史会话)\n"
	MsgNoHistoryMsg = "(当前会话无消息)\n"
	MsgCancelled    = "已取消\n"

	MsgDialogueEmpty = "用法: 直接输入内容与 AI 对话（: 前缀亦可）\n"
)

const (
	MsgInterruptKept = "已中断，本回合已完成步骤已保留，继续输入可续接\n"
	MsgInterruptBare = "已中断\n"
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

	MsgCurTheme  = "当前主题: %s\n"
	MsgThemeHead = "可用主题:\n"
	MsgThemeSet  = "主题已切换为 %s（%s）\n"
	MsgThemeBad  = "无效主题 %q（可用: %s）"
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
	TurnSepTimeFmt = "──── %s"
	TurnSepDurFmt  = " · 回合 %s"
)

const (
	MsgLinesTotal = "共 %d 行"
	MsgLines      = "%d 行"
	MsgCachePct   = "缓存 %.2f%%"
	MsgCtxTokens  = "上下文 ~%s"
	MsgTruncNote  = "…中间省略 %d 字节…"
	MsgInterrupt  = "已中断"
	MsgNotStarted = "未执行"
	MsgTimeout    = "执行超时"
	MsgSuspended  = "挂起已终止"
	MsgToolErr    = "错误: %s"

	MsgInteractiveHint = "  ⏎ 等待终端输入，请在下方直接应答\n"
)

const (
	MsgMdOn  = "Markdown 渲染已开启\n"
	MsgMdOff = "Markdown 渲染已关闭\n"
)

const (
	SpinWaiting  = "%s 等待响应 %s"
	SpinThinking = "%s 思考中 %s"
	SpinRunning  = "  %s 执行中 %s"
)

const helpText = `斜杠命令：
  /help            显示帮助
  /new             开启新会话（当前会话自动保存）
  /load [id]       无参打开会话选择菜单；带 id 直接载入
  /context         显示上下文占用
  /history [n|all] 无参截断列表；n 全量查看单条；all 全量显示
  /model [name]    无参显示当前模型；带名切换模型
  /think [level]   无参显示思考等级；设置 minimal/low/medium/high/max，off 关闭
  /theme [name]    无参显示当前主题与可用列表；带名切换内置主题
  /md              切换 AI 输出 Markdown 渲染（默认开启，非 TTY 自动旁路）
  /exit            退出
输入分发：
  内容 / :内容     与 AI 对话（两种写法等价，全角 ： 亦可）
  /命令            斜杠命令
  exit / quit      退出
`

const welcomLogo = `
██████ ▄████▄ ███  ██ ██  ██ ▄████▄ 
  ██   ██▄▄██ ██ ▀▄██  ▀██▀  ██▄▄██ 
  ██   ██  ██ ██   ██   ██   ██  ██ 
`

const MsgNoSaveWarn = "不落盘模式：本次会话不写入会话文件"

var (
	Version   = "dev"
	BuildTime = ""
)

func welcomeText() string {
	ver := "tanyan " + Version
	if BuildTime != "" {
		ver += "（构建于 " + BuildTime + "）"
	}
	return welcomLogo + "\n输入 /help 查看命令   " + ver + "\n\n"
}

const (
	FlagConfig  = "配置文件路径（默认 ~/.config/tanyan/config.yaml）"
	FlagMode    = "会话存储模式 local/global/auto（默认 auto）"
	FlagNoSave  = "会话只读：不写入会话文件（历史会话仍可列出与载入）"
	FlagPlain   = "纯文本输出：无颜色/无动画/无工具块与状态行，stdout 只留答案与命令反馈（供父代理解析）"
	FlagVerbose = "与 --plain 同用：恢复工具块与状态行的纯文本形态"

	MsgVerboseRequiresPlain = "--verbose 需与 --plain 同用"
	FlagVersion             = "显示版本号并退出"
)
