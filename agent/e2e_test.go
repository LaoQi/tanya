package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAskSingleTurn(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "这是回答"})
	a := newAgent(t, m)
	var sb strings.Builder
	sink := EventSink(func(e Event) {
		if e.Kind == EventContent {
			sb.WriteString(e.Text)
		}
	})
	if err := a.Ask(context.Background(), "问题", sink); err != nil {
		t.Fatal(err)
	}
	if sb.String() != "这是回答" {
		t.Errorf("输出: %q", sb.String())
	}
	if len(a.history) != 2 {
		t.Fatalf("history: %d", len(a.history))
	}
	if len(m.reqs) != 1 {
		t.Fatalf("请求数: %d", len(m.reqs))
	}
	if m.reqs[0].Messages[0].Role != "system" {
		t.Error("首条应为 system prompt")
	}
}

func TestAskShellToolLoop(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "run_shell", args: `{"command":"echo hello-tool"}`}}},
		mockStep{content: "执行完毕"},
	)
	a := newAgent(t, m)
	var toolEvents []string
	var startEvents []string
	sink := EventSink(func(e Event) {
		switch e.Kind {
		case EventToolStart:
			startEvents = append(startEvents, e.ToolName+":"+e.ToolArgs)
		case EventToolEnd:
			toolEvents = append(toolEvents, e.ToolName)
		}
	})

	if err := a.Ask(context.Background(), "测试 shell", sink); err != nil {
		t.Fatal(err)
	}
	if len(a.history) != 4 {
		t.Fatalf("history: %d", len(a.history))
	}
	toolMsg := a.history[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_1" || toolMsg.Name != "run_shell" {
		t.Errorf("tool 消息异常: %+v", toolMsg)
	}
	if !strings.Contains(toolMsg.Content, "hello-tool") {
		t.Errorf("tool 结果: %q", toolMsg.Content)
	}
	if len(toolEvents) != 1 || toolEvents[0] != "run_shell" {
		t.Errorf("OnToolEnd 回调: %v", toolEvents)
	}
	if len(startEvents) != 1 || !strings.Contains(startEvents[0], "hello-tool") {
		t.Errorf("OnToolStart 回调: %v", startEvents)
	}
	if len(m.reqs) != 2 {
		t.Fatalf("请求数: %d", len(m.reqs))
	}
	second := m.reqs[1].Messages
	if len(second) != 4 || second[3].Role != "tool" {
		t.Fatalf("第二次请求应回填 tool 结果: %+v", second)
	}
}

func TestAskNonShellToolLoop(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "calc", args: `{"expression":"6*7"}`}}},
		mockStep{content: "42"},
	)
	a := newAgent(t, m)
	if err := a.Ask(context.Background(), "算一下", nil); err != nil {
		t.Fatal(err)
	}
	if a.history[2].Content != "42" {
		t.Errorf("非 shell 工具结果应回填: %q", a.history[2].Content)
	}
}

func TestAskUsageFallback(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回答"})
	a := newAgent(t, m)
	if st := a.Stats(); st.HasContext {
		t.Errorf("无 usage 时不应标记实报: %+v", st)
	}
	if err := a.Ask(context.Background(), "问题", nil); err != nil {
		t.Fatal(err)
	}
	if st := a.Stats(); st.HasContext {
		t.Errorf("对端未返回 usage 时仍不应标记实报: %+v", st)
	}
}

func TestAskUsageReal(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回答", usage: &Usage{PromptTokens: 1500, CompletionTokens: 10, TotalTokens: 1510}})
	a := newAgent(t, m)
	if err := a.Ask(context.Background(), "问题", nil); err != nil {
		t.Fatal(err)
	}
	if st := a.Stats(); !st.HasContext || st.ContextTokens != 1500 {
		t.Errorf("应记录实报 prompt tokens: %+v", st)
	}
}

func TestAskRequestCallbacks(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "calc", args: `{"expression":"1+1"}`}}},
		mockStep{content: "2", usage: &Usage{PromptTokens: 1200, CompletionTokens: 5, TotalTokens: 1205}},
	)
	a := newAgent(t, m)
	var starts int
	var infos []ResponseInfo
	sink := EventSink(func(e Event) {
		switch e.Kind {
		case EventRequestStart:
			starts++
		case EventResponse:
			infos = append(infos, e.Response)
		}
	})
	if err := a.Ask(context.Background(), "算一下", sink); err != nil {
		t.Fatal(err)
	}
	if starts != 2 || len(infos) != 2 {
		t.Fatalf("回调次数: starts=%d responses=%d", starts, len(infos))
	}
	for i, info := range infos {
		if info.Duration <= 0 {
			t.Errorf("infos[%d] Duration 应 >0: %v", i, info.Duration)
		}
	}
	if infos[0].Usage != nil {
		t.Errorf("首轮无 usage，应本地估算: %+v", infos[0].Usage)
	}
	if infos[0].ContextTokens <= 0 {
		t.Errorf("首轮 ContextTokens 应本地估算 >0: %d", infos[0].ContextTokens)
	}
	if infos[1].Usage == nil || infos[1].Usage.PromptTokens != 1200 {
		t.Errorf("次轮 Usage 透传: %+v", infos[1].Usage)
	}
	if infos[1].ContextTokens != 1200 {
		t.Errorf("次轮 ContextTokens 应取 prompt tokens: %d", infos[1].ContextTokens)
	}
}

func TestAskRequestCallbackOnError(t *testing.T) {
	m := newMockLLM(t, mockStep{status: 500})
	a := newAgent(t, m)
	var starts int
	var infos []ResponseInfo
	sink := EventSink(func(e Event) {
		switch e.Kind {
		case EventRequestStart:
			starts++
		case EventResponse:
			infos = append(infos, e.Response)
		}
	})
	if err := a.Ask(context.Background(), "问题", sink); err == nil {
		t.Fatal("应返回错误")
	}
	if starts != 1 || len(infos) != 1 {
		t.Fatalf("出错路径也应回调: starts=%d responses=%d", starts, len(infos))
	}
	if infos[0].Duration <= 0 || infos[0].Usage != nil {
		t.Errorf("出错路径 info: %+v", infos[0])
	}
}

func TestAskUnknownTool(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "hack", args: `{}`}}},
		mockStep{content: "end"},
	)
	a := newAgent(t, m)
	if err := a.Ask(context.Background(), "go", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.history[2].Content, "未知工具") {
		t.Errorf("got %q", a.history[2].Content)
	}
}

func milestoneKinds(kinds []EventKind) []EventKind {
	var out []EventKind
	for _, k := range kinds {
		switch k {
		case EventRequestStart, EventResponse, EventToolStart, EventToolEnd:
			out = append(out, k)
		}
	}
	return out
}

func TestAskEventSequence(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "calc", args: `{"expression":"1+1"}`}}},
		mockStep{content: "2"},
	)
	a := newAgent(t, m)
	var kinds []EventKind
	if err := a.Ask(context.Background(), "算一下", EventSink(func(e Event) { kinds = append(kinds, e.Kind) })); err != nil {
		t.Fatal(err)
	}
	got := milestoneKinds(kinds)
	want := []EventKind{EventRequestStart, EventResponse, EventToolStart, EventToolEnd, EventRequestStart, EventResponse}
	if len(got) != len(want) {
		t.Fatalf("里程碑事件序列: %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("里程碑事件序列: %v want %v", got, want)
		}
	}
}

func TestAskReasoningPhaseEvents(t *testing.T) {
	m := newMockLLM(t, mockStep{reasoning: "先推理", content: "结论"})
	a := newAgent(t, m)
	var order []EventKind
	var info ResponseInfo
	sink := EventSink(func(e Event) {
		switch e.Kind {
		case EventReasoning, EventContent:
			order = append(order, e.Kind)
		case EventResponse:
			info = e.Response
		}
	})
	if err := a.Ask(context.Background(), "问题", sink); err != nil {
		t.Fatal(err)
	}
	if len(order) < 2 || order[0] != EventReasoning {
		t.Fatalf("思考事件应先于正文: %v", order)
	}
	if order[len(order)-1] != EventContent {
		t.Errorf("末尾应为正文事件: %v", order)
	}
	if info.Duration <= 0 {
		t.Errorf("Duration 应 >0: %v", info.Duration)
	}
}

func TestAskToolEventsCarryResult(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "run_shell", args: `{"command":"echo evt"}`}}},
		mockStep{content: "done"},
	)
	a := newAgent(t, m)
	var startBeforeEnd bool
	var sawResult bool
	var started bool
	sink := EventSink(func(e Event) {
		switch e.Kind {
		case EventToolStart:
			started = true
			if e.ToolName != "run_shell" || !strings.Contains(e.ToolArgs, "echo evt") {
				t.Errorf("ToolStart 载荷异常: %+v", e)
			}
		case EventToolEnd:
			if started {
				startBeforeEnd = true
			}
			if meta, ok := e.Result.Meta.(*stubShellMeta); !ok || meta == nil || !strings.Contains(e.Result.Text, "echo evt") {
				t.Errorf("ToolEnd 应携带结构化结果: %+v", e.Result)
			}
			sawResult = true
		}
	})
	if err := a.Ask(context.Background(), "跑一下", sink); err != nil {
		t.Fatal(err)
	}
	if !startBeforeEnd || !sawResult {
		t.Errorf("ToolStart 应先于 ToolEnd 且携带结果: start=%v result=%v", startBeforeEnd, sawResult)
	}
}

func TestRunTurnInteractiveEventPassthrough(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "run_shell", args: `{"command":"echo hi","interactive":true}`}}},
		mockStep{toolCalls: []mockToolCall{{id: "call_2", name: "run_shell", args: `{"command":"echo hi"}`}}},
		mockStep{content: "完成"},
	)
	a := newAgent(t, m)
	type toolEvent struct {
		name        string
		interactive bool
	}
	var starts, ends []toolEvent
	sink := EventSink(func(e Event) {
		switch e.Kind {
		case EventToolStart:
			starts = append(starts, toolEvent{e.ToolName, e.Interactive})
		case EventToolEnd:
			ends = append(ends, toolEvent{e.ToolName, e.Interactive})
		}
	})
	if err := a.Ask(context.Background(), "测试", sink); err != nil {
		t.Fatal(err)
	}
	if len(starts) != 2 || len(ends) != 2 {
		t.Fatalf("工具事件数: start=%d end=%d", len(starts), len(ends))
	}
	for i, want := range []bool{true, false} {
		if starts[i].name != "run_shell" || ends[i].name != "run_shell" {
			t.Errorf("第 %d 次事件工具名: %q / %q", i+1, starts[i].name, ends[i].name)
		}
		if starts[i].interactive != want || ends[i].interactive != want {
			t.Errorf("第 %d 次 Interactive: start=%v end=%v want %v",
				i+1, starts[i].interactive, ends[i].interactive, want)
		}
	}
}

func TestRunTurnBadJSONArgs(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "run_shell", args: `{"command":`}}},
		mockStep{content: "已忽略"},
	)
	a := newAgent(t, m)
	var interactive []bool
	sink := EventSink(func(e Event) {
		if e.Kind == EventToolStart {
			interactive = append(interactive, e.Interactive)
		}
	})
	if err := a.Ask(context.Background(), "测试", sink); err != nil {
		t.Fatal(err)
	}
	if len(interactive) != 1 || interactive[0] {
		t.Errorf("坏 JSON 时事件 Interactive 应为 false: %v", interactive)
	}
	var toolMsg string
	for _, msg := range a.history {
		if msg.Role == "tool" {
			toolMsg = msg.Content
		}
	}
	if !strings.Contains(toolMsg, "参数解析失败") {
		t.Errorf("tool 结果应为参数解析失败: %q", toolMsg)
	}
}

func TestAskChatReplaysReasoningContent(t *testing.T) {
	m := newMockLLM(t,
		mockStep{reasoning: "先看看目录", toolCalls: []mockToolCall{{id: "call_1", name: "run_shell", args: `{"command":"echo hi"}`}}},
		mockStep{reasoning: "再作答", content: "完成"},
	)
	a := newAgent(t, m)
	if err := a.Ask(context.Background(), "跑一下", nil); err != nil {
		t.Fatal(err)
	}
	if len(a.history[1].ReasoningItems) != 1 || a.history[1].ReasoningItems[0].Content != "先看看目录" {
		t.Fatalf("chat 历史应落思维链: %+v", a.history[1].ReasoningItems)
	}
	if len(m.reqs) != 2 {
		t.Fatalf("请求数: %d", len(m.reqs))
	}
	second := m.reqs[1].Messages
	if len(second) != 4 || second[0].Role != "system" || second[2].Role != "assistant" {
		t.Fatalf("第二轮请求消息异常: %+v", second)
	}
	if second[2].ReasoningContent != "先看看目录" {
		t.Errorf("第二轮应回传 reasoning_content: %+v", second[2])
	}
	if len(second[2].ReasoningItems) != 0 {
		t.Errorf("wire 上不应出现 reasoning_items: %+v", second[2])
	}
}

func TestAskEmitsTurnEndOnce(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "call_1", name: "run_shell", args: `{"command":"echo turn-end"}`}}},
		mockStep{content: "完成"},
	)
	a := newAgent(t, m)
	var ends []TurnInfo
	var order []EventKind
	sink := EventSink(func(e Event) {
		order = append(order, e.Kind)
		if e.Kind == EventTurnEnd {
			ends = append(ends, e.Turn)
		}
	})
	if err := a.Ask(context.Background(), "跑", sink); err != nil {
		t.Fatal(err)
	}
	if len(ends) != 1 {
		t.Fatalf("含工具调用的回合（两次请求）应恰好一次回合结束事件: %d", len(ends))
	}
	if ends[0].Duration <= 0 || ends[0].Failed || ends[0].Interrupted {
		t.Errorf("成功回合的结算异常: %+v", ends[0])
	}
	if order[len(order)-1] != EventTurnEnd {
		t.Errorf("回合结束事件应是最后一个: %v", order)
	}
}

func TestAskTurnEndFlagsOnFailureAndInterrupt(t *testing.T) {
	m := newMockLLM(t, mockStep{status: 500})
	a := newAgent(t, m)
	var got []TurnInfo
	sink := EventSink(func(e Event) {
		if e.Kind == EventTurnEnd {
			got = append(got, e.Turn)
		}
	})
	if err := a.Ask(context.Background(), "问题", sink); err == nil {
		t.Fatal("应返回错误")
	}
	if len(got) != 1 || !got[0].Failed || got[0].Interrupted {
		t.Errorf("失败回合应标 Failed 且非 Interrupted: %+v", got)
	}

	m2 := newMockLLM(t, mockStep{toolCalls: []mockToolCall{{id: "c1", name: "run_shell", args: `{"command":"sleep 30"}`}}})
	a2 := newAgent(t, m2)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	got = nil
	err := a2.Ask(ctx, "问题", EventSink(func(e Event) {
		if e.Kind == EventTurnEnd {
			got = append(got, e.Turn)
		}
	}))
	var ie *InterruptError
	if !errors.As(err, &ie) {
		t.Fatalf("应返回中断错误: %v", err)
	}
	if len(got) != 1 || !got[0].Interrupted || got[0].Failed {
		t.Errorf("中断回合应标 Interrupted 且非 Failed: %+v", got)
	}
}
