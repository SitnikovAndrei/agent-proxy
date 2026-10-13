package ocm

import (
	"database/sql"
	"errors"
	"time"

	"ocm/internal/statistics"
)

type usageTracker struct{ store *statistics.Store }

func (u *usageTracker) AddUsageWithCycleHint(model string, window int, input, output, cached int64, tier statistics.ServiceTier, user string, observed time.Time, cycle *WeeklyCycleHint) error {
	week := deriveWeekStartUnix(cycle)
	stats := UsageStats{RequestCount: 1, InputTokens: input, OutputTokens: output, CachedTokens: cached}
	if stats.InputTokens < 0 || stats.CachedTokens < 0 || stats.CachedTokens > stats.InputTokens {
		return errors.New("cached tokens must be between zero and input tokens")
	}
	record := statistics.Record{
		ObservedAt: observed,
		Provider:   statistics.OpenAI, Model: model, ContextWindow: window,
		Tier:      sql.Null[statistics.ServiceTier]{V: tier, Valid: true},
		Username:  sql.Null[string]{V: user, Valid: user != ""},
		WeekStart: sql.Null[int64]{V: week, Valid: week != 0},
		Counters: statistics.Counters{Requests: int64(stats.RequestCount), InputTokens: stats.InputTokens - stats.CachedTokens,
			OutputTokens: stats.OutputTokens, CacheReadTokens: stats.CachedTokens},
	}
	if err := record.Validate(); err != nil {
		return err
	}
	record.EstimatedCostUSD = calculateCost(stats, model, tier, window)
	return u.store.Record(record)
}

func deriveWeekStartUnix(cycle *WeeklyCycleHint) int64 {
	if cycle == nil || cycle.WindowMinutes <= 0 || cycle.ResetAt.IsZero() {
		return 0
	}
	return cycle.ResetAt.UTC().Add(-time.Duration(cycle.WindowMinutes) * time.Minute).Unix()
}
