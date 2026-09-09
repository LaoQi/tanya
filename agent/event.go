package agent

type EventKind uint8

const (
	EventRequestStart EventKind = iota + 1
	EventReasoning
	EventContent
	EventToolCall
	EventUsage
	EventToolStart
	EventToolEnd
	EventResponse
)

type Event struct {
	Kind      EventKind
	Text      string
	ToolIndex int
	ToolID    string
	ToolName  string
	ToolArgs  string
	Usage     *Usage
	Result    ToolResult
	Response  ResponseInfo
}

type EventSink func(Event)

func (s EventSink) Emit(e Event) {
	if s != nil {
		s(e)
	}
}
