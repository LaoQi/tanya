package agent

import "testing"

func TestUsageStatsView(t *testing.T) {
	var s usageStats
	if got := s.view(); got != (Stats{}) {
		t.Errorf("零值快照应全空: %+v", got)
	}
	s.record(&Usage{TotalTokens: 900, PromptTokens: 800, CompletionTokens: 100, CacheHitTokens: 300})
	s.record(&Usage{TotalTokens: 1100, PromptTokens: 1000, CompletionTokens: 100, PromptTokensDetails: &promptTokensDetails{CachedTokens: 700}})
	got := s.view()
	if got.PromptTokens != 1800 || got.CompletionTokens != 200 || got.TotalTokens != 2000 || got.CacheHitTokens != 1000 {
		t.Errorf("累计项应按全部请求求和: %+v", got)
	}
	if !got.HasContext || got.ContextTokens != 1000 {
		t.Errorf("上下文应取最近一次实报: %+v", got)
	}
	if got.ContextHit != 700 {
		t.Errorf("单次命中量应取最近一次: %+v", got)
	}
	s.reset()
	if after := s.view(); after != (Stats{}) {
		t.Errorf("reset 后应清空全部状态: %+v", after)
	}
}
