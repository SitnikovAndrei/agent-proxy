package statistics

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

const filteredUsage = ` FROM usage_records WHERE provider = :provider AND username = :user
 AND (:model IS NULL OR model = :model)
 AND (:from_time IS NULL OR replace(observed_at_utc,'Z','') >= :from_time)
 AND (:to_time IS NULL OR replace(observed_at_utc,'Z','') < :to_time)`

const totalsColumns = `
 COALESCE(SUM(request_count),0), COALESCE(SUM(messages_count),0),
 COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
 COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_creation_tokens),0),
 COALESCE(SUM(cache_creation_5m_tokens),0), COALESCE(SUM(cache_creation_1h_tokens),0),
 COALESCE(SUM(input_tokens+cache_read_tokens+cache_creation_tokens),0),
 ROUND(COALESCE(SUM(estimated_cost_usd),0),6)`

func (s *Store) Summary(ctx context.Context, filter Filter) (Summary, error) {
	if err := filter.Validate(); err != nil {
		return Summary{}, err
	}
	provider, err := filter.Provider.wireValue()
	if err != nil {
		return Summary{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Summary{}, err
	}
	defer tx.Rollback()
	args := []any{sql.Named("provider", provider), sql.Named("user", filter.Username), sql.Named("model", filter.Model),
		sql.Named("from_time", timestampBound(filter.From)), sql.Named("to_time", timestampBound(filter.To))}
	result := Summary{Provider: provider, User: filter.Username, Limit: filter.Limit, Offset: filter.Offset}
	if err := tx.QueryRowContext(ctx, "SELECT "+totalsColumns+filteredUsage, args...).Scan(result.Totals.destinations()...); err != nil {
		return Summary{}, err
	}
	result.ByModel, result.HasMore, err = queryModels(ctx, tx, filter, args)
	if err != nil {
		return Summary{}, err
	}
	if err := tx.Commit(); err != nil {
		return Summary{}, err
	}
	return result, nil
}

func queryModels(ctx context.Context, tx *sql.Tx, filter Filter, args []any) ([]ModelSummary, bool, error) {
	args = append(args, sql.Named("limit", filter.Limit+1), sql.Named("offset", filter.Offset))
	rows, err := tx.QueryContext(ctx, `SELECT model,service_tier,context_window,`+totalsColumns+filteredUsage+
		` GROUP BY model,service_tier,context_window ORDER BY model,service_tier,context_window LIMIT :limit OFFSET :offset`, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	models := make([]ModelSummary, 0)
	for rows.Next() {
		var model ModelSummary
		var tier sql.NullString
		destinations := append([]any{&model.Model, &tier, &model.ContextWindow}, model.Totals.destinations()...)
		if err := rows.Scan(destinations...); err != nil {
			return nil, false, err
		}
		if tier.Valid {
			parsed, err := ParseServiceTier(tier.String)
			if err != nil {
				return nil, false, err
			}
			model.Tier = &parsed
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(models) > filter.Limit
	if more {
		models = models[:filter.Limit]
	}
	return models, more, nil
}

func (t *Totals) destinations() []any {
	return []any{&t.Requests, &t.Messages, &t.InputTokens, &t.OutputTokens, &t.CacheReadTokens, &t.CacheCreationTokens,
		&t.CacheCreation5mTokens, &t.CacheCreation1hTokens, &t.TotalInputTokens, &t.EstimatedCostUSD}
}

// Removing the UTC suffix preserves chronological text ordering for variable-length fractions.
func timestampBound(value sql.Null[time.Time]) sql.NullString {
	if !value.Valid {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.TrimSuffix(value.V.UTC().Format(time.RFC3339Nano), "Z"), Valid: true}
}
