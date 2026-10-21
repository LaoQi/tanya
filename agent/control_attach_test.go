package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestAgentToolAttachGet(t *testing.T) {
	ops := &stubAttachOps{list: "当前工作区的运行实例（/tmp/run）:\n（无）\n"}
	st := &stubConfigTarget{ops: ops}
	if got := invokeAgentTool(t, st, `{"action":"get","key":"attach"}`); got != ops.list {
		t.Errorf("get attach = %q, 期望原样透传 %q", got, ops.list)
	}
}

func TestAgentToolAttachNotInjected(t *testing.T) {
	st := &stubConfigTarget{}
	for _, args := range []string{`{"action":"get","key":"attach"}`, `{"action":"prune","key":"attach"}`} {
		got := invokeAgentTool(t, st, args)
		if !strings.HasPrefix(got, MsgErrPrefix) || !strings.Contains(got, MsgControlAttachAbsent) {
			t.Errorf("%s 未注入应报能力不可用: %q", args, got)
		}
	}
}

func TestAgentToolPruneAttach(t *testing.T) {
	ops := &stubAttachOps{prune: "清理 1 项；保留 0 项\n"}
	st := &stubConfigTarget{ops: ops}
	got := invokeAgentTool(t, st, `{"action":"prune","key":"attach"}`)
	if got != ops.prune {
		t.Errorf("prune attach = %q, 期望 %q", got, ops.prune)
	}
	if ops.np != 1 {
		t.Errorf("应调用 Prune 一次, got %d", ops.np)
	}
}

func TestAgentToolPruneRejectsOtherKeys(t *testing.T) {
	st := &stubConfigTarget{model: "m", ops: &stubAttachOps{}}
	for _, key := range []string{"model", "reasoning_effort", "usage", "sessions", "config_path"} {
		got := invokeAgentTool(t, st, `{"action":"prune","key":"`+key+`"}`)
		if !strings.HasPrefix(got, MsgErrPrefix) || !strings.Contains(got, "只适用于 key=attach") {
			t.Errorf("prune %s 应被拒绝: %q", key, got)
		}
	}
	if st.model != "m" {
		t.Errorf("被拒绝的 prune 不得改动状态: %q", st.model)
	}
}

func TestAgentToolPruneRejectsValue(t *testing.T) {
	ops := &stubAttachOps{}
	st := &stubConfigTarget{ops: ops}
	got := invokeAgentTool(t, st, `{"action":"prune","key":"attach","value":"yes"}`)
	if !strings.Contains(got, "不需要 value") {
		t.Errorf("prune 带 value 应被拒绝: %q", got)
	}
	if ops.np != 0 {
		t.Errorf("被拒绝时不得执行清理: %d", ops.np)
	}
}

func TestAgentToolPruneErrorTransparent(t *testing.T) {
	st := &stubConfigTarget{ops: &stubAttachOps{err: errors.New("磁盘只读")}}
	got := invokeAgentTool(t, st, `{"action":"prune","key":"attach"}`)
	if !strings.HasPrefix(got, MsgErrPrefix) || !strings.Contains(got, "磁盘只读") {
		t.Errorf("错误应原样透传: %q", got)
	}
}

func TestAgentToolBadActionLabelIncludesPrune(t *testing.T) {
	st := &stubConfigTarget{}
	got := invokeAgentTool(t, st, `{"action":"purge","key":"attach"}`)
	if !strings.Contains(got, "get/set/prune") {
		t.Errorf("未知 action 应列出 prune: %q", got)
	}
}

func TestAgentWithAttachOps(t *testing.T) {
	a := newTestAgent(t)
	if a.AttachOps() != nil {
		t.Fatal("默认不应注入 attach 能力")
	}
	ops := &stubAttachOps{}
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	a2, err := New(cfg, WithAttachOps(ops))
	if err != nil {
		t.Fatal(err)
	}
	if a2.AttachOps() != AttachOps(ops) {
		t.Fatal("WithAttachOps 应被采纳")
	}
}
