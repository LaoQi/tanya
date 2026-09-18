package agent

type usageStats struct {
	contextTokens int
	contextHit    int
	hasContext    bool
	totals        Usage
}

func (s *usageStats) record(u *Usage) {
	s.contextTokens = u.PromptTokens
	s.contextHit = u.CacheHit()
	s.hasContext = true
	s.totals.PromptTokens += u.PromptTokens
	s.totals.CompletionTokens += u.CompletionTokens
	s.totals.TotalTokens += u.TotalTokens
	s.totals.CacheHitTokens += u.CacheHit()
}

func (s *usageStats) reset() {
	*s = usageStats{}
}

func (s usageStats) view() Stats {
	return Stats{
		ContextTokens:    s.contextTokens,
		ContextHit:       s.contextHit,
		HasContext:       s.hasContext,
		PromptTokens:     s.totals.PromptTokens,
		CompletionTokens: s.totals.CompletionTokens,
		TotalTokens:      s.totals.TotalTokens,
		CacheHitTokens:   s.totals.CacheHitTokens,
	}
}

type Stats struct {
	Workspace        string
	Session          string
	Archived         string
	Messages         int
	Est              int
	ContextTokens    int
	ContextHit       int
	HasContext       bool
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheHitTokens   int
}
