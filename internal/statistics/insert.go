package statistics

import (
	"context"
	"database/sql"
	"time"
)

func insertRecord(ctx context.Context, db *sql.DB, r Record) error {
	provider, err := r.Provider.wireValue()
	if err != nil {
		return err
	}
	tier := sql.NullString{}
	if r.Tier.Valid {
		value, err := r.Tier.V.WireValue()
		if err != nil {
			return err
		}
		tier = sql.NullString{String: value, Valid: true}
	}
	observed := r.ObservedAt.UTC().Format(time.RFC3339Nano)
	c := r.Counters
	_, err = db.ExecContext(ctx, `INSERT INTO usage_records
 (provider,model,service_tier,context_window,username,observed_at_utc,week_start_unix,
 request_count,messages_count,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,
 cache_creation_5m_tokens,cache_creation_1h_tokens,estimated_cost_usd)
 VALUES (:provider,:model,:tier,:window,:username,:observed,:week,
 :requests,:messages,:input,:output,:read,:creation,:creation5m,:creation1h,:cost)`,
		sql.Named("provider", provider), sql.Named("model", r.Model), sql.Named("tier", tier),
		sql.Named("window", r.ContextWindow), sql.Named("username", r.Username), sql.Named("observed", observed),
		sql.Named("week", r.WeekStart), sql.Named("requests", c.Requests),
		sql.Named("messages", c.Messages), sql.Named("input", c.InputTokens), sql.Named("output", c.OutputTokens),
		sql.Named("read", c.CacheReadTokens), sql.Named("creation", c.CacheCreationTokens),
		sql.Named("creation5m", c.CacheCreation5mTokens), sql.Named("creation1h", c.CacheCreation1hTokens),
		sql.Named("cost", r.EstimatedCostUSD))
	return err
}
