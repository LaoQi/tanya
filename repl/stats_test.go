package repl

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanya/agent"
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

func TestCacheRateUnavailable(t *testing.T) {
	for _, c := range []struct{ hit, prompt int }{{0, 1200}, {500, 0}, {0, 0}, {-1, 100}} {
		if _, ok := cacheRate(c.hit, c.prompt); ok {
			t.Errorf("hit=%d prompt=%d 不应可算率", c.hit, c.prompt)
		}
		if got := formatRate(c.hit, c.prompt); got != "" {
			t.Errorf("hit=%d prompt=%d 应渲染为空: %q", c.hit, c.prompt, got)
		}
	}
}

func TestCacheRateValues(t *testing.T) {
	if got := formatRate(980, 1200); got != "81.67%" {
		t.Errorf("DeepSeek 风格: %q", got)
	}
	if got := formatRate(1580, 2400); got != "65.83%" {
		t.Errorf("累计口径: %q", got)
	}
	if got := formatRate(1200, 1200); got != "100.00%" {
		t.Errorf("满命中: %q", got)
	}
}

func TestUsageText(t *testing.T) {
	if got := usageText(agent.Stats{Est: 1500}); got != "~1.5k" {
		t.Errorf("无实报应回落估算: %q", got)
	}
	if got := usageText(agent.Stats{HasContext: true, ContextTokens: 1200, Est: 1500}); got != "1.2k" {
		t.Errorf("实报应直接显示: %q", got)
	}
	if got := usageText(agent.Stats{HasContext: true, Est: 1500}); got != "~1.5k" {
		t.Errorf("实报为 0 应视同无数据: %q", got)
	}
}

func TestCacheText(t *testing.T) {
	if got := cacheText(agent.Stats{}); got != "" {
		t.Errorf("无数据应为空: %q", got)
	}
	if got := cacheText(agent.Stats{HasContext: true, ContextTokens: 1200, ContextHit: 980}); got != "980" {
		t.Errorf("应显示本次请求命中量: %q", got)
	}
	if got := cacheText(agent.Stats{ContextHit: 500}); got != "" {
		t.Errorf("有命中无分母应与命中率一致按无数据: %q", got)
	}
	if got := cacheText(agent.Stats{HasContext: true, ContextTokens: 1200, CacheHitTokens: 1580, PromptTokens: 2400}); got != "" {
		t.Errorf("累计命中不得泄漏进单次变量: %q", got)
	}
}

func TestCacheRateText(t *testing.T) {
	if got := cacheRateText(agent.Stats{HasContext: true, ContextTokens: 1200, ContextHit: 980}); got != "81.67%" {
		t.Errorf("单次命中率: %q", got)
	}
	if got := cacheRateText(agent.Stats{HasContext: true, ContextTokens: 1200, CacheHitTokens: 1580, PromptTokens: 2400}); got != "" {
		t.Errorf("累计命中不得泄漏进单次变量: %q", got)
	}
}

func TestUsageTotalText(t *testing.T) {
	if got := usageTotalText(agent.Stats{Est: 1500}); got != "" {
		t.Errorf("无 usage 应为空: %q", got)
	}
	if got := usageTotalText(agent.Stats{TotalTokens: 2000, PromptTokens: 1800, CompletionTokens: 200}); got != "2.0k" {
		t.Errorf("应显示累计用量: %q", got)
	}
}

func TestCacheTotalText(t *testing.T) {
	if got := cacheTotalText(agent.Stats{}); got != "" {
		t.Errorf("无数据应为空: %q", got)
	}
	if got := cacheTotalText(agent.Stats{CacheHitTokens: 1580, PromptTokens: 2400}); got != "1.6k" {
		t.Errorf("应显示累计命中量: %q", got)
	}
	if got := cacheTotalText(agent.Stats{HasContext: true, ContextTokens: 1200, ContextHit: 980}); got != "" {
		t.Errorf("单次命中不得泄漏进累计变量: %q", got)
	}
}

func TestCacheRateTotalText(t *testing.T) {
	if got := cacheRateTotalText(agent.Stats{CacheHitTokens: 1580, PromptTokens: 2400}); got != "65.83%" {
		t.Errorf("累计命中率: %q", got)
	}
	if got := cacheRateTotalText(agent.Stats{HasContext: true, ContextTokens: 1200, ContextHit: 980}); got != "" {
		t.Errorf("单次命中不得泄漏进累计变量: %q", got)
	}
}

func TestSummaryText(t *testing.T) {
	if got := summaryText(agent.Stats{Est: 1500}); got != "~1.5k" {
		t.Errorf("无缓存应回落估算: %q", got)
	}
	if got := summaryText(agent.Stats{HasContext: true, ContextTokens: 1200, Est: 1}); got != "1.2k" {
		t.Errorf("无缓存应回落实报用量: %q", got)
	}
	got := summaryText(agent.Stats{HasContext: true, ContextTokens: 1200, CacheHitTokens: 1580, PromptTokens: 2400})
	if got != "1.2k 65.83%" {
		t.Errorf("用量应取上下文大小、命中率取累计: %q", got)
	}
	got = summaryText(agent.Stats{HasContext: true, ContextTokens: 1200, Est: 9999, CacheHitTokens: 400, PromptTokens: 3200})
	if got != "1.2k 12.50%" {
		t.Errorf("用量不得回落累计: %q", got)
	}
}

func TestStatInfoEmpty(t *testing.T) {
	info := statInfo(agent.Stats{Workspace: "/w", Session: "/tmp/s.jsonl", Messages: 3, Est: 1500})
	for _, w := range []string{
		"工作区: /w",
		"会话文件: /tmp/s.jsonl",
		"消息: 3 条",
		"总用量: 无（未收到 API usage）",
		"上下文: ~1.5k（本地估算）",
		"缓存: 无数据",
		"命中率: 无数据",
	} {
		if !strings.Contains(info, w) {
			t.Errorf("statInfo 缺 %q: %q", w, info)
		}
	}
}

func TestStatInfoReported(t *testing.T) {
	info := statInfo(agent.Stats{
		Workspace: "/w", Messages: 3, HasContext: true, ContextTokens: 1000,
		PromptTokens: 1800, CompletionTokens: 200, TotalTokens: 2000, CacheHitTokens: 1000,
	})
	for _, w := range []string{
		"总用量: 2.0k（prompt 1.8k / completion 200）",
		"上下文: 1.0k（API 实报）",
		"缓存: 1.0k",
		"命中率: 55.56%",
	} {
		if !strings.Contains(info, w) {
			t.Errorf("statInfo 缺 %q: %q", w, info)
		}
	}
}

func TestResolveVarsPlaceholders(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	r, _, _ := newTestREPLAgent(t, a, newFakeTerm())
	vars := r.resolveVars()
	for _, name := range []string{
		"cwd", "model", "effort", "usage", "cache", "cache_rate",
		"usage_total", "cache_total", "cache_rate_total", "usage_summary",
	} {
		if _, ok := vars(name); !ok {
			t.Errorf("占位符 %s 未注册", name)
		}
	}
	if _, ok := vars("bogus"); ok {
		t.Error("未知占位符不应注册")
	}
	usage, _ := vars("usage")
	if !strings.HasPrefix(usage, "~") {
		t.Errorf("无实报时 usage 应为估算: %q", usage)
	}
	for _, name := range []string{"cache", "cache_rate", "usage_total", "cache_total", "cache_rate_total"} {
		if v, _ := vars(name); v != "" {
			t.Errorf("零状态 %s 应渲染为空: %q", name, v)
		}
	}
	if stat, _ := vars("usage_summary"); stat != usage {
		t.Errorf("零状态 stat 应等于 usage: %q vs %q", stat, usage)
	}
}
