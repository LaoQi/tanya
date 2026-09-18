package repl

const (
	MsgErrLineFmt   = "错误: %v"
	MsgAskUsage     = "用法: tanya ask \"问题\"\n"
	MsgNoSessions   = "(无历史会话)\n"
	MsgNoHistoryMsg = "(当前会话无消息)\n"
	MsgCancelled    = "已取消\n"

	MsgDialogueEmpty = "用法: 直接输入内容与 AI 对话（: 前缀亦可）\n"
)

const (
	MsgInitHead       = "初始化工作区 %s\n"
	MsgInitAskIgnore  = "是否为 .tanya/ 建立忽略文件（内容 *，避免会话入库）？[y/N] "
	MsgInitTagNew     = "新建"
	MsgInitTagOld     = "已有"
	MsgInitTagSkip    = "跳过"
	MsgInitEntryFmt   = "  %s  %s  %s\n"
	MsgInitSessionFmt = "  会话目录 %s\n"
	MsgInitHint       = "提示: 直接描述需求即可开工，可让 AI 读完目录后补全 AGENTS.md\n"
	MsgInitUsage      = "用法: tanya init\n"
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

	MsgCurReasoning    = "思维链显示: %s\n"
	MsgReasoningOn     = "思维链显示已开启\n"
	MsgReasoningHidden = "思维链显示已开启（当前输出档不显示思维链）\n"
	MsgReasoningOff    = "思维链显示已关闭\n"
	MsgReasoningBad    = "无效参数 %q（可用: on / off）"
	MsgReasoningOnTag  = "开"
	MsgReasoningOffTag = "关"
	MsgReasonHead      = "思考"
	MsgReasonTail      = "思考结束"
	MsgReasonDurFmt    = " · %s"

	reasonRuleLeft  = "─── "
	reasonRuleRight = " ───"

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
	MsgArchiveDone        = "已归档 %d 个会话 → %s（%s → %s）\n"
	MsgArchivePreview     = "将归档 %d 个会话（约 %s）\n"
	MsgArchiveNone        = "没有符合条件的会话\n"
	MsgArchiveNoneKeep    = "没有需要归档的会话（活跃会话数未超过保留数）\n"
	MsgArchiveNoneWindow  = "没有早于 %s 未活动的会话\n"
	MsgArchiveConfirm     = "现在归档？[y/N] "
	MsgArchiveCancel      = "已取消，未归档\n"
	MsgArchiveOnlyTTY     = "归档仅在交互终端下可用（当前为非交互或纯文本模式）\n"
	MsgArchiveSkipFmt     = "  跳过 %s（%s）\n"
	MsgArchiveFailFmt     = "  失败 %s（%v）\n"
	MsgArchiveBadArg      = "无效的归档参数 %q（保留数量如 20、0，或时长如 7d、12h）"
	MsgForkDone           = "已 fork 为新会话 %s\n"
	MsgForkNotArchive     = "当前会话不是归档只读会话，直接对话即可\n"
	MsgForkNoSave         = "（不落盘模式，未写入）\n"
	MsgLoadArchived       = "已载入会话 %s（归档只读，继续对话请 /fork）\n"
	MsgArchiveReadOnlyFmt = "当前为归档只读会话（%s）；继续对话请 /fork 开新会话\n"
	MsgAutoArchiveAsk     = "当前工作区有 %d 个活跃会话（阈值 %d），建议归档较早的，只保留最近 %d 个。\n将归档 %d 个会话（约 %s）。现在归档？[y/N] "
	MsgAutoArchiveSkip    = "已跳过（配置 auto_archive: false 可关闭此提示，或随时 /archive 手动归档）\n"
	SessArchMark          = "[归档] "
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
	MsgStatWorkspace      = "工作区: %s"
	MsgStatSession        = "会话文件: %s"
	MsgStatSessionArchive = "会话文件: 归档只读 %s（未写入）"
	MsgStatMessages       = "消息: %d 条"
	MsgStatTotals         = "总用量: %s"
	MsgStatTotalsFmt      = "%s（prompt %s / completion %s）"
	MsgStatContext        = "上下文: %s"
	MsgStatContextAPI     = "%s（API 实报）"
	MsgStatContextEst     = "~%s（本地估算）"
	MsgStatCache          = "缓存: %s"
	MsgStatHitRate        = "命中率: %s"
	MsgStatNoUsage        = "无（未收到 API usage）"
	MsgStatNoCache        = "无数据"
)

const (
	MsgLinesTotal = "共 %d 行"
	MsgLines      = "%d 行"
	MsgCachePct   = "缓存 %s"
	MsgCtxTokens  = "上下文 ~%s"
	MsgTruncNote  = "…中间省略 %d 字节…"
	MsgInterrupt  = "已中断"
	MsgNotStarted = "未执行"
	MsgTimeout    = "执行超时"
	MsgSuspended  = "挂起已终止"
	MsgToolErr    = "错误: %s"

	MsgInteractiveHint = "  ⏎ 等待终端输入，请在下方直接应答\n"
	MsgCmdOmittedFmt   = "… 省略 %d 行（完整命令见 /history）"
)

const (
	MsgBadTheme = "无效主题 %q（可用: %s）"
)

const (
	MsgStatusWaiting  = "» 等待响应"
	MsgStatusThinking = "» 思考中"
	MsgStatusRunning  = "» 执行中"
)

const (
	MsgFarewellSessionFmt = "会话 %s · 时长 %s · 消息 %d 条"
	MsgFarewellTimeFmt    = "时长 %s · 消息 %d 条"
	MsgFarewellUsageFmt   = "用量 %s"
	MsgFarewellCacheTail  = "· 缓存 %s"
	MsgFarewellFileFmt    = "会话文件 %s"
	MsgFarewellNoFile     = "会话文件 未写入（不落盘模式）"
)

const helpText = `斜杠命令：
  /help               显示帮助
  /new                开启新会话（当前会话自动保存）
  /load [id]          无参打开会话选择菜单，带 id 直接载入
  /archive [n|<时长>] 归档历史会话（先出报告再确认；无参 = 保留 auto_archive_keep 个，纯数字 = 保留 n 个（0 = 全部），7d/12h = 按未活动时长）
  /fork               把归档只读会话 fork 成新会话（继承历史）
  /stat               会话统计（工作区/用量/缓存）
  /history [n|all]    无参截断列表，n 查看单条，all 全量显示
  /model [name]       显示或切换模型
  /think [level]      显示或设置思考等级（minimal/low/medium/high/max/off）
  /reasoning [on|off] 显示或切换思维链回传
  /theme [name]       显示或切换内置主题
直接输入内容即可与 AI 对话
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
	ver := "tanya " + Version
	if BuildTime != "" {
		ver += "（构建于 " + BuildTime + "）"
	}
	return welcomLogo + "\n输入 /help 查看命令   " + ver + "\n\n"
}

const (
	FlagConfig  = "配置文件路径（默认 ~/.config/tanya/config.yaml）"
	FlagMode    = "会话存储模式 local/global/auto（默认 auto）"
	FlagNoSave  = "会话只读：不写入会话文件（历史会话仍可列出与载入）"
	FlagPlain   = "纯文本输出：无颜色/无动画/无工具块与状态行，stdout 只留答案与命令反馈（供父代理解析）"
	FlagVerbose = "与 --plain 同用：恢复工具块与状态行的纯文本形态"

	MsgVerboseRequiresPlain = "--verbose 需与 --plain 同用"
	FlagVersion             = "显示版本号并退出"
)
