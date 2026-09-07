package repl

import (
	"strings"
	"testing"
)

func TestHandleCommandThink(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	r := &REPL{agent: a}

	out := captureStdout(func() { r.handleCommand("/think") })
	if out != MsgThinkUnset {
		t.Errorf("无参未设置: got %q want %q", out, MsgThinkUnset)
	}

	out = captureStdout(func() { r.handleCommand("/think high") })
	if want := "思考等级已设为 high\n"; out != want {
		t.Errorf("设置: got %q want %q", out, want)
	}
	if a.ReasoningEffort() != "high" {
		t.Errorf("agent 未生效: %q", a.ReasoningEffort())
	}

	out = captureStdout(func() { r.handleCommand("/think") })
	if want := "思考等级: high\n"; out != want {
		t.Errorf("查看: got %q want %q", out, want)
	}

	out = captureStdout(func() { r.handleCommand("/think bogus") })
	if !strings.Contains(out, "无效思考等级") {
		t.Errorf("非法值应报错: %q", out)
	}
	if a.ReasoningEffort() != "high" {
		t.Errorf("失败后不应变更: %q", a.ReasoningEffort())
	}

	out = captureStdout(func() { r.handleCommand("/think off") })
	if out != MsgEffortOff {
		t.Errorf("off: got %q want %q", out, MsgEffortOff)
	}
	if a.ReasoningEffort() != "" {
		t.Errorf("off 未清空: %q", a.ReasoningEffort())
	}

	out = captureStdout(func() { r.handleCommand("/think max") })
	if want := "思考等级已设为 max\n"; out != want {
		t.Errorf("max: got %q want %q", out, want)
	}
}
