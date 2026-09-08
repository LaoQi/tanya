package agent

const (
	MsgErrPrefix   = "error: "
	MsgErrLine     = MsgErrPrefix + "%s"
	MsgParseArgs   = MsgErrPrefix + "参数解析失败: %v"
	MsgUnknownTool = MsgErrPrefix + "未知工具 %s"
	MsgInterrupted = MsgErrPrefix + "已中断"
	MsgTimedOut    = MsgErrPrefix + "执行超时"
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
	MsgBadSessionID     = "非法会话 id"
	MsgSessionGone      = "会话不存在: %s"
	MsgShellUnavailable = "run_shell 不可用（未找到可执行 shell）"
	MsgBadEffort        = "无效思考等级 %q（可选: minimal/low/medium/high/max/off）"
	MsgBadApiProtocol   = "无效 api_protocol %q（可选: chat/responses）"
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

const (
	MsgTokenAPI      = "token: %d（prompt %d / completion %d，API 实报）"
	MsgTokenEstimate = "token: ~%d（本地估算）"
	MsgContextInfo   = "%s\n消息: %d 条\n会话文件: %s"
)

const (
	MsgBadTheme = "无效主题 %q（可用: %s）"
)
