package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAskErrorNoOutputRollsBack(t *testing.T) {
	m := newMockLLM(t, mockStep{status: 500})
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "问题", nil); err == nil {
		t.Fatal("应返回错误")
	}
	if len(a.history) != 0 {
		t.Fatalf("无产出错误后 history 应回滚为空: %d", len(a.history))
	}
}

func TestAskErrorKeepsPartialTurn(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "get_time", args: `{}`}}},
		mockStep{status: 500},
	)
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	err = a.Ask(context.Background(), "问题", nil)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if len(a.history) != 4 {
		t.Fatalf("应保留 user+assistant+tool+错误提示 共4条: %d", len(a.history))
	}
	last := a.history[3]
	if last.Role != "user" || !strings.HasPrefix(last.Content, "[本轮因错误中止") {
		t.Errorf("末条应为错误提示: %+v", last)
	}
}

func TestAskInterruptKeepsPartialTurn(t *testing.T) {
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
	err = a.Ask(ctx, "问题", nil)
	var ie *InterruptError
	if !errors.As(err, &ie) {
		t.Fatalf("应返回中断错误: %v", err)
	}
	if !ie.Kept {
		t.Fatal("有工具产出应保留")
	}
	if ie.Error() != MsgInterruptedBare {
		t.Errorf("中断错误文案不应带前缀: %q", ie.Error())
	}
	if !errors.Is(err, context.Canceled) {
		t.Error("中断错误应可解包为 context.Canceled")
	}
	if len(a.history) != 4 {
		t.Fatalf("应保留 user+assistant+tool+中断提示 共4条: %d", len(a.history))
	}
	if a.history[3].Role != "user" || a.history[3].Content != MsgInterruptNotice {
		t.Errorf("末条应为中断提示: %+v", a.history[3])
	}
}

func TestAskInterruptNoOutputRollsBack(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "回复", hold: 300 * time.Millisecond})
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	err = a.Ask(ctx, "问题", nil)
	var ie *InterruptError
	if !errors.As(err, &ie) {
		t.Fatalf("应返回中断错误: %v", err)
	}
	if ie.Kept {
		t.Fatal("无产出不应保留")
	}
	if len(a.history) != 0 {
		t.Fatalf("无产出中断后 history 应回滚为空: %d", len(a.history))
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
		t.Fatalf("无产出错误应仅保留第一轮 2 条消息: %d", len(a.history))
	}
	if a.history[1].Content != "第一轮回答" {
		t.Errorf("第一轮回答被破坏: %+v", a.history[1])
	}
}

func TestNoticeTurnPersisted(t *testing.T) {
	m := newMockLLM(t, mockStep{toolCalls: []mockToolCall{{id: "c1", name: "run_shell", args: `{"command":"sleep 30"}`}}})
	cfg := m.config()
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	if err := a.Ask(ctx, "问题", nil); err == nil {
		t.Fatal("中断应返回错误")
	}
	data, err := os.ReadFile(a.store.path())
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	var msgs []Message
	for {
		var msg Message
		if err := dec.Decode(&msg); err != nil {
			break
		}
		msgs = append(msgs, msg)
	}
	if len(msgs) != 4 {
		t.Fatalf("空提示词下不应落快照行，会话文件应为 4 条消息: %d", len(msgs))
	}
	for _, m := range msgs {
		if m.Role == "system" {
			t.Errorf("空提示词不应写入 system 快照行: %+v", m)
		}
	}
	if msgs[3].Content != MsgInterruptNotice {
		t.Errorf("文件末条应为中断提示: %+v", msgs[3])
	}
	a2, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a2.LoadSession(strings.TrimSuffix(filepath.Base(a.store.path()), ".jsonl")); err != nil {
		t.Fatal(err)
	}
	if len(a2.history) != 4 {
		t.Fatalf("载入后应为 4 条: %d", len(a2.history))
	}
	if a2.history[3].Content != MsgInterruptNotice {
		t.Errorf("载入后末条应为中断提示: %+v", a2.history[3])
	}
}

func TestSaveWriteFailureKeepsCursor(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/full 仅 linux 可用")
	}
	m := newMockLLM(t)
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	a.store.file = "/dev/full"
	a.history = []Message{{Role: "user", Content: "x"}}
	if err := a.save(); err == nil {
		t.Fatal("写入 /dev/full 应失败")
	}
	if a.store.saved != 0 {
		t.Fatalf("失败时 saved 不应推进: %d", a.store.saved)
	}
	if a.store.systemSaved {
		t.Fatal("失败时 systemSaved 不应置位")
	}
}

func TestAskInterruptMultiToolPartial(t *testing.T) {
	m := newMockLLM(t, mockStep{toolCalls: []mockToolCall{
		{id: "c1", name: "run_shell", args: `{"command":"sleep 30"}`},
		{id: "c2", name: "run_shell", args: `{"command":"sleep 30"}`},
	}})
	a, err := New(m.config())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	err = a.Ask(ctx, "跑两个", nil)
	var ie *InterruptError
	if !errors.As(err, &ie) || !ie.Kept {
		t.Fatalf("应中断并保留产出: %v", err)
	}
	if len(a.history) != 5 {
		t.Fatalf("应保留 user+assistant+tool+tool+中断提示 共5条: %d", len(a.history))
	}
	if !strings.Contains(a.history[2].Content, "输出可能不完整") {
		t.Errorf("首个工具应标记运行中中断: %q", a.history[2].Content)
	}
	if !strings.Contains(a.history[3].Content, "命令未执行") {
		t.Errorf("后续工具应标记未执行: %q", a.history[3].Content)
	}
}
