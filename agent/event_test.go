package agent

import "testing"

func TestEventSinkNilSafe(t *testing.T) {
	var s EventSink
	s.Emit(Event{Kind: EventContent, Text: "x"})
}

func TestEventSinkDelivers(t *testing.T) {
	var got []EventKind
	s := EventSink(func(e Event) { got = append(got, e.Kind) })
	s.Emit(Event{Kind: EventReasoning})
	s.Emit(Event{Kind: EventContent})
	if len(got) != 2 || got[0] != EventReasoning || got[1] != EventContent {
		t.Errorf("事件应按序投递: %v", got)
	}
}
