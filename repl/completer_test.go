package repl

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
)

func testCompleter(infos []agent.SessionInfo) *completer {
	return &completer{listSessions: func() ([]agent.SessionInfo, error) {
		return infos, nil
	}}
}

func TestSuggestLoadIDOnly(t *testing.T) {
	c := testCompleter([]agent.SessionInfo{{ID: "20260101-100000"}})
	if got := c.suggest("/load 20"); got != "260101-100000" {
		t.Errorf("ghost 应仅补 ID: %q", got)
	}
	if got := c.suggest("/load 99"); got != "" {
		t.Errorf("无匹配应为空: %q", got)
	}
	if got := c.suggest("/load 20260101-100000"); got != "" {
		t.Errorf("ID 已完整应为空: %q", got)
	}
}

func TestSuggestCommandContext(t *testing.T) {
	c := &completer{}
	if got := c.suggest("/he"); got != "lp" {
		t.Errorf("got %q", got)
	}
}

func testModelsCompleter(ids []string, err error, calls *int) *completer {
	return &completer{listModels: func() ([]string, error) {
		*calls++
		return ids, err
	}}
}

func TestSuggestModelContext(t *testing.T) {
	calls := 0
	c := testModelsCompleter([]string{"auto-flash", "auto-flash-pro", "glm-5"}, nil, &calls)
	if got := c.suggest("/model aut"); got != "o-flash" {
		t.Errorf("got %q", got)
	}
}

func TestSuggestThinkContext(t *testing.T) {
	c := &completer{}
	if got := c.suggest("/think hi"); got != "gh" {
		t.Errorf("got %q", got)
	}
	if got := c.suggest("/think of"); got != "f" {
		t.Errorf("off 候选: %q", got)
	}
	if got := c.suggest("/think off"); got != "" {
		t.Errorf("已完整应为空: %q", got)
	}
}

func TestCompleteThinkContext(t *testing.T) {
	c := &completer{}
	cands := c.complete("/think ")
	if len(cands) != len(agent.EffortLevels)+1 {
		t.Fatalf("应为全部等级加 off: %v", cands)
	}
	if cands[0].Insert != "/think minimal" || cands[0].Display != "minimal" {
		t.Errorf("got %+v", cands[0])
	}
	if cands[len(cands)-1].Insert != "/think off" {
		t.Errorf("末位应为 off: %+v", cands[len(cands)-1])
	}
	cands = c.complete("/think m")
	if len(cands) != 3 {
		t.Fatalf("m 应匹配 minimal/medium/max: %v", cands)
	}
}

func TestCompleteModelContext(t *testing.T) {
	calls := 0
	c := testModelsCompleter([]string{"auto-flash", "auto-flash-pro"}, nil, &calls)
	cands := c.complete("/model ")
	if len(cands) != 2 {
		t.Fatalf("got %v", cands)
	}
	if cands[0].Insert != "/model auto-flash" || cands[0].Display != "auto-flash" {
		t.Errorf("got %+v", cands[0])
	}
}

func TestModelsCacheOnce(t *testing.T) {
	calls := 0
	c := testModelsCompleter([]string{"m1"}, nil, &calls)
	c.models()
	c.models()
	c.complete("/model ")
	if calls != 1 {
		t.Errorf("模型列表应只加载一次: %d", calls)
	}
}

func TestModelsFailureNoRetry(t *testing.T) {
	calls := 0
	c := testModelsCompleter(nil, errors.New("down"), &calls)
	if got := c.models(); got != nil {
		t.Errorf("失败应返回空: %v", got)
	}
	c.models()
	if calls != 1 {
		t.Errorf("失败也不应重试: %d", calls)
	}
}

func TestCompleteLoadShowsSummary(t *testing.T) {
	c := testCompleter([]agent.SessionInfo{{
		ID:      "20260101-100000",
		ModTime: time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local),
		Msgs:    3,
		Summary: "测试摘要",
	}})
	cands := c.complete("/load 2")
	if len(cands) != 1 {
		t.Fatalf("应返回 1 个候选: %v", cands)
	}
	if cands[0].Insert != "/load 20260101-100000" {
		t.Errorf("Insert 应仅含 ID: %q", cands[0].Insert)
	}
	want := "/load 20260101-100000  01-01 10:00    3条  测试摘要"
	if cands[0].Display != want {
		t.Errorf("Display got %q want %q", cands[0].Display, want)
	}
}

func TestCompleteCommandContext(t *testing.T) {
	c := &completer{}
	cands := c.complete("/he")
	if len(cands) != 1 || cands[0].Insert != "/help" {
		t.Errorf("got %v", cands)
	}
	if cands := c.complete("/h"); len(cands) != 2 {
		t.Errorf("/h 应匹配 help 与 history: %v", cands)
	}
}

func TestCompleteLoadEmptyPrefix(t *testing.T) {
	c := testCompleter([]agent.SessionInfo{
		{ID: "20260904-215919", ModTime: time.Date(2026, 9, 4, 21, 59, 0, 0, time.Local), Msgs: 2, Summary: "说一句话"},
		{ID: "20260904-214341", ModTime: time.Date(2026, 9, 4, 21, 43, 0, 0, time.Local), Msgs: 1, Summary: "另一句"},
	})
	cands := c.complete("/load ")
	if len(cands) != 2 {
		t.Fatalf("应返回 2 个候选: %v", cands)
	}
	if strings.Contains(cands[0].Display, "214341") {
		t.Errorf("候选应按列表顺序: %q", cands[0].Display)
	}
}
