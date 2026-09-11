package repl

// Kind 标记每次输出的类别，是噪音门禁与测试断言的把手（不导出包外、不进 agent.Event）。
type Kind uint8

const (
	KindContent    Kind = iota + 1 // assistant 正文（流式 + 回放）
	KindReasoning                  // 思维链（当前不上屏，预留）
	KindToolBlock                  // 工具标题/正文块（含其结构性空行）
	KindToolStatus                 // 工具状态行与响应状态行（↳ …）
	KindNotice                     // 信息性文案：命令反馈、回放列表
	KindDecor                      // 纯装饰：欢迎屏、回合分隔线
	KindError                      // 错误与中断提示
	KindSpinner                    // 动画帧与清行
)
