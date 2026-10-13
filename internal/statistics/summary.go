package statistics

type Totals struct {
	Requests              int64   `json:"request_count"`
	Messages              int64   `json:"messages_count"`
	InputTokens           int64   `json:"input_tokens"`
	OutputTokens          int64   `json:"output_tokens"`
	CacheReadTokens       int64   `json:"cache_read_tokens"`
	CacheCreationTokens   int64   `json:"cache_creation_tokens"`
	CacheCreation5mTokens int64   `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int64   `json:"cache_creation_1h_tokens"`
	TotalInputTokens      int64   `json:"total_input_tokens"`
	EstimatedCostUSD      float64 `json:"estimated_cost_usd"`
}

type ModelSummary struct {
	Model         string       `json:"model"`
	Tier          *ServiceTier `json:"service_tier,omitempty"`
	ContextWindow int          `json:"context_window"`
	Totals
}

type Summary struct {
	Provider string         `json:"provider"`
	User     string         `json:"user"`
	Totals   Totals         `json:"totals"`
	ByModel  []ModelSummary `json:"by_model"`
	Limit    int            `json:"limit"`
	Offset   int            `json:"offset"`
	HasMore  bool           `json:"has_more"`
}
