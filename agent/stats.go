package agent

import "fmt"

type usageStats struct{ last *Usage }

func (s *usageStats) record(u *Usage) { s.last = u }

func (s *usageStats) reset() { s.last = nil }

func (s usageStats) contextInfo(est, msgs int, sessionPath string) string {
	var tokenLine string
	if s.last != nil {
		tokenLine = fmt.Sprintf(MsgTokenAPI,
			s.last.TotalTokens, s.last.PromptTokens, s.last.CompletionTokens)
	} else {
		tokenLine = fmt.Sprintf(MsgTokenEstimate, est)
	}
	return fmt.Sprintf(MsgContextInfo, tokenLine, msgs, sessionPath)
}

func (s usageStats) promptUsage(est int) string {
	if s.last != nil {
		return formatTokens(s.last.PromptTokens)
	}
	return "~" + formatTokens(est)
}

func (s usageStats) promptCache() string {
	if s.last == nil {
		return ""
	}
	hit := s.last.CacheHit()
	if hit <= 0 {
		return ""
	}
	return formatTokens(hit)
}

func (s usageStats) promptCacheRate() string {
	if s.last == nil {
		return ""
	}
	hit := s.last.CacheHit()
	if hit <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2f%%", float64(hit)/float64(s.last.PromptTokens)*100)
}

func (s usageStats) summary(est int) string {
	if s.last == nil || s.last.CacheHit() <= 0 {
		return s.promptUsage(est)
	}
	return formatTokens(s.last.CacheHit()) + "/" + formatTokens(s.last.PromptTokens) + " " + s.promptCacheRate()
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}
