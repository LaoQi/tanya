package agent

import "time"

type EventKind uint8

const (
	EventRequestStart EventKind = iota + 1
	EventReasoning
	EventContent
	EventToolCall // 当前无消费者，属预留（并行工具，见 docs/shell-tool.md）
	EventToolStart
	EventToolEnd
	EventResponse
	EventReasoningEnd
	EventTurnEnd
)

type Event struct {
	Kind        EventKind
	Text        string
	ToolIndex   int
	ToolID      string
	ToolName    string
	ToolArgs    string
	Result      ToolResult
	Interactive bool
	Response    ResponseInfo
	Turn        TurnInfo
}

// TurnInfo 是一次对话回合的结算；Duration 的起点只有 Ask 知道，属事实。
type TurnInfo struct {
	Duration    time.Duration
	Failed      bool
	Interrupted bool
}

type EventSink func(Event)

func (s EventSink) Emit(e Event) {
	if s != nil {
		s(e)
	}
}

// Sinks 把若干 sink 合成一个：同步串行、装配期固定、顺序即因果。
// 消费者不得阻塞（慢消费者自缓冲）、不得 panic 逃逸、不得反向调用 agent。
func Sinks(list ...EventSink) EventSink {
	live := make([]EventSink, 0, len(list))
	for _, s := range list {
		if s != nil {
			live = append(live, s)
		}
	}
	switch len(live) {
	case 0:
		return nil
	case 1:
		return live[0]
	}
	return func(e Event) {
		for _, s := range live {
			s(e)
		}
	}
}
