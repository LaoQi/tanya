package builtin

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
