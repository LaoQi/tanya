package agent

import (
	"context"
	"testing"
	"time"
)

func TestAskErrorRollbackHistory(t *testing.T) {
	m := newMockLLM(t, mockStep{status: 500})
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "问题", nil); err == nil {
		t.Fatal("应返回错误")
	}
	if len(a.history) != 0 {
		t.Fatalf("出错后 history 应回滚为空: %d", len(a.history))
	}
}

func TestAskInterruptRollbackHistory(t *testing.T) {
	m := newMockLLM(t, mockStep{toolCalls: []mockToolCall{{id: "c1", name: "run_shell", args: `{"command":"sleep 30"}`}}})
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	if err := a.Ask(ctx, "问题", nil); err == nil {
		t.Fatal("中断后应返回错误")
	}
	if len(a.history) != 0 {
		t.Fatalf("中断后 history 应回滚为空: %d", len(a.history))
	}
}

func TestAskRollbackKeepsPreviousTurn(t *testing.T) {
	m := newMockLLM(t,
		mockStep{content: "第一轮回答"},
		mockStep{status: 500},
	)
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "第一问", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "第二问", nil); err == nil {
		t.Fatal("第二轮应返回错误")
	}
	if len(a.history) != 2 {
		t.Fatalf("应仅保留第一轮 2 条消息: %d", len(a.history))
	}
	if a.history[1].Content != "第一轮回答" {
		t.Errorf("第一轮回答被破坏: %+v", a.history[1])
	}
}
