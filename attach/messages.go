package attach

const (
	MsgBadMagic   = "attach: 握手失败（魔数不符）"
	MsgBadVersion = "attach: 协议版本不匹配（本机 %d，对端 %d）"
	MsgPeerGone   = "attach: 目标已断开或退出: %w"
	MsgDial       = "attach: 连接目标失败: %w"
	MsgListen     = "attach: 监听运行实例失败: %w"

	MsgForceTimeout = "attach: force 接管超时——目标未响应中断（回合可能已卡死）"
)
