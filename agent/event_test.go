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

func TestSinksFanOut(t *testing.T) {
	var a, b []EventKind
	s := Sinks(
		func(e Event) { a = append(a, e.Kind) },
		nil,
		func(e Event) { b = append(b, e.Kind) },
	)
	s.Emit(Event{Kind: EventContent})
	s.Emit(Event{Kind: EventTurnEnd})
	if len(a) != 2 || len(b) != 2 {
		t.Fatalf("两个消费者都应收到全部事件: a=%v b=%v", a, b)
	}
	if a[0] != b[0] || a[1] != b[1] {
		t.Errorf("扇出应同步串行、顺序一致: a=%v b=%v", a, b)
	}
}

func TestSinksDegenerate(t *testing.T) {
	if got := Sinks(); got != nil {
		t.Errorf("无消费者应返回 nil: %v", got)
	}
	if got := Sinks(nil, nil); got != nil {
		t.Errorf("全 nil 应返回 nil（门禁零开销）: %v", got)
	}
	single := EventSink(func(Event) {})
	if got := Sinks(nil, single); got == nil {
		t.Error("单个消费者应原样返回而非包装")
	}
}
