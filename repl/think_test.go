package repl

import (
	"strings"
	"testing"
)

func TestHandleCommandThink(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	run := func(cmd string) string {
		out.Reset()
		errb.Reset()
		r.handleCommand(cmd)
		return out.String()
	}

	if got := run("/think"); got != MsgThinkUnset {
		t.Errorf("无参未设置: got %q want %q", got, MsgThinkUnset)
	}
	if got := run("/think high"); got != "思考等级已设为 high\n" {
		t.Errorf("设置: got %q want %q", got, "思考等级已设为 high\n")
	}
	if a.ReasoningEffort() != "high" {
		t.Errorf("agent 未生效: %q", a.ReasoningEffort())
	}
	if got := run("/think"); got != "思考等级: high\n" {
		t.Errorf("查看: got %q want %q", got, "思考等级: high\n")
	}
	run("/think bogus")
	if !strings.Contains(errb.String(), "无效思考等级") {
		t.Errorf("非法值应报错到 stderr: %q", errb.String())
	}
	if a.ReasoningEffort() != "high" {
		t.Errorf("失败后不应变更: %q", a.ReasoningEffort())
	}
	if got := run("/think off"); got != MsgEffortOff {
		t.Errorf("off: got %q want %q", got, MsgEffortOff)
	}
	if a.ReasoningEffort() != "" {
		t.Errorf("off 未清空: %q", a.ReasoningEffort())
	}
	if got := run("/think max"); got != "思考等级已设为 max\n" {
		t.Errorf("max: got %q want %q", got, "思考等级已设为 max\n")
	}
}
