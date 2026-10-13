package ccm

import (
	"database/sql"

	"ocm/internal/statistics"
)

type usageTracker struct{ store *statistics.Store }

func (u *usageTracker) record(usage usageRecord) error {
	model, window, user, week, stats := usage.model, usage.contextWindow, usage.username, usage.weekStartUnix, usage.stats
	record := statistics.Record{
		ObservedAt: usage.observedAt,
		Provider:   statistics.Anthropic, Model: model, ContextWindow: window,
		Username:  sql.Null[string]{V: user, Valid: user != ""},
		WeekStart: sql.Null[int64]{V: week, Valid: week != 0},
		Counters: statistics.Counters{Requests: int64(stats.RequestCount), Messages: int64(stats.MessagesCount),
			InputTokens: stats.InputTokens, OutputTokens: stats.OutputTokens, CacheReadTokens: stats.CacheReadInputTokens,
			CacheCreationTokens: stats.CacheCreationInputTokens, CacheCreation5mTokens: stats.CacheCreation5MinuteInputTokens,
			CacheCreation1hTokens: stats.CacheCreation1HourInputTokens},
	}
	if err := record.Validate(); err != nil {
		return err
	}
	record.EstimatedCostUSD = calculateCost(stats, model, window)
	return u.store.Record(record)
}
