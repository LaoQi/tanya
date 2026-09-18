package repl

import (
	"fmt"
	"strings"

	"github.com/LaoQi/tanya/agent"
)

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func cacheRate(hit, prompt int) (float64, bool) {
	if hit <= 0 || prompt <= 0 {
		return 0, false
	}
	return float64(hit) / float64(prompt) * 100, true
}

func formatRate(hit, prompt int) string {
	rate, ok := cacheRate(hit, prompt)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.2f%%", rate)
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fk", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

func usageText(st agent.Stats) string {
	if st.HasContext && st.ContextTokens > 0 {
		return formatTokens(st.ContextTokens)
	}
	return "~" + formatTokens(st.Est)
}

func cacheText(st agent.Stats) string {
	if _, ok := cacheRate(st.ContextHit, st.ContextTokens); !ok {
		return ""
	}
	return formatTokens(st.ContextHit)
}

func cacheRateText(st agent.Stats) string {
	return formatRate(st.ContextHit, st.ContextTokens)
}

func usageTotalText(st agent.Stats) string {
	if st.PromptTokens == 0 && st.CompletionTokens == 0 && st.TotalTokens == 0 {
		return ""
	}
	return formatTokens(st.TotalTokens)
}

func cacheTotalText(st agent.Stats) string {
	if _, ok := cacheRate(st.CacheHitTokens, st.PromptTokens); !ok {
		return ""
	}
	return formatTokens(st.CacheHitTokens)
}

func cacheRateTotalText(st agent.Stats) string {
	return formatRate(st.CacheHitTokens, st.PromptTokens)
}

func summaryText(st agent.Stats) string {
	text := usageText(st)
	if rate := cacheRateTotalText(st); rate != "" {
		return text + " " + rate
	}
	return text
}

func totalsText(st agent.Stats) string {
	if st.PromptTokens == 0 && st.CompletionTokens == 0 && st.TotalTokens == 0 {
		return MsgStatNoUsage
	}
	return fmt.Sprintf(MsgStatTotalsFmt,
		formatTokens(st.TotalTokens),
		formatTokens(st.PromptTokens),
		formatTokens(st.CompletionTokens))
}

func contextText(st agent.Stats) string {
	if st.HasContext && st.ContextTokens > 0 {
		return fmt.Sprintf(MsgStatContextAPI, formatTokens(st.ContextTokens))
	}
	return fmt.Sprintf(MsgStatContextEst, formatTokens(st.Est))
}

func statInfo(st agent.Stats) string {
	var b strings.Builder
	fmt.Fprintf(&b, MsgStatWorkspace+"\n", st.Workspace)
	if st.Archived != "" {
		fmt.Fprintf(&b, MsgStatSessionArchive+"\n", st.Archived)
	} else {
		fmt.Fprintf(&b, MsgStatSession+"\n", st.Session)
	}
	fmt.Fprintf(&b, MsgStatMessages+"\n", st.Messages)
	fmt.Fprintf(&b, MsgStatTotals+"\n", totalsText(st))
	fmt.Fprintf(&b, MsgStatContext+"\n", contextText(st))
	fmt.Fprintf(&b, MsgStatCache+"\n", cacheTextOrNone(st))
	fmt.Fprintf(&b, MsgStatHitRate, rateTextOrNone(st))
	return b.String()
}

func cacheTextOrNone(st agent.Stats) string {
	if c := cacheTotalText(st); c != "" {
		return c
	}
	return MsgStatNoCache
}

func rateTextOrNone(st agent.Stats) string {
	if r := cacheRateTotalText(st); r != "" {
		return r
	}
	return MsgStatNoCache
}
