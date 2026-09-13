package agent

import (
	"strings"
	"testing"
)

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{12345, "12.3k"},
		{1000000, "1000.0k"},
	}
	for _, c := range cases {
		if got := formatTokens(c.in); got != c.want {
			t.Errorf("formatTokens(%d) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestUsageStatsZeroValue(t *testing.T) {
	var s usageStats
	if s.promptCache() != "" || s.promptCacheRate() != "" {
		t.Error("零值不应有缓存数据")
	}
	if got := s.promptUsage(1500); got != "~1.5k" {
		t.Errorf("零值应回落估算: %q", got)
	}
	if got := s.summary(1500); got != "~1.5k" {
		t.Errorf("零值 summary 应回落估算: %q", got)
	}
	info := s.contextInfo(1500, 3, "/tmp/s.jsonl")
	for _, w := range []string{"token: ~1500（本地估算）", "消息: 3 条", "会话文件: /tmp/s.jsonl"} {
		if !strings.Contains(info, w) {
			t.Errorf("contextInfo 缺 %q: %q", w, info)
		}
	}
}

func TestUsageStatsAPIReported(t *testing.T) {
	s := usageStats{last: &Usage{TotalTokens: 900, PromptTokens: 800, CompletionTokens: 100}}
	info := s.contextInfo(1, 3, "/tmp/s.jsonl")
	if !strings.Contains(info, "token: 900（prompt 800 / completion 100，API 实报）") {
		t.Errorf("有 usage 应报 API 实报值: %q", info)
	}
	if got := s.promptUsage(1); got != "800" {
		t.Errorf("promptUsage 应用 API 值: %q", got)
	}
}

func TestUsageStatsReset(t *testing.T) {
	var s usageStats
	s.record(&Usage{PromptTokens: 1200, CacheHitTokens: 980})
	if s.promptCache() != "980" {
		t.Fatal("record 未生效")
	}
	s.reset()
	if s.last != nil || s.promptCache() != "" {
		t.Errorf("reset 后应清空: %+v", s.last)
	}
	if got := s.promptUsage(2000); got != "~2.0k" {
		t.Errorf("reset 后应回落估算: %q", got)
	}
}
