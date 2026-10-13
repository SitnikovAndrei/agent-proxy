package statistics

const schema = `
CREATE TABLE IF NOT EXISTS usage_records (
 id INTEGER PRIMARY KEY,
 provider TEXT NOT NULL CHECK (provider IN ('openai', 'anthropic')),
 model TEXT NOT NULL CHECK (length(model) > 0),
 service_tier TEXT CHECK (service_tier IN ('default','auto','flex','priority','scale')),
 context_window INTEGER NOT NULL CHECK (context_window > 0),
 username TEXT CHECK (length(username) > 0),
 observed_at_utc TEXT NOT NULL,
 week_start_unix INTEGER CHECK (week_start_unix > 0),
 request_count INTEGER NOT NULL CHECK (request_count >= 0),
 messages_count INTEGER NOT NULL CHECK (messages_count >= 0),
 input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
 output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
 cache_read_tokens INTEGER NOT NULL CHECK (cache_read_tokens >= 0),
 cache_creation_tokens INTEGER NOT NULL CHECK (cache_creation_tokens >= 0),
 cache_creation_5m_tokens INTEGER NOT NULL CHECK (cache_creation_5m_tokens >= 0),
 cache_creation_1h_tokens INTEGER NOT NULL CHECK (cache_creation_1h_tokens >= 0),
 estimated_cost_usd REAL NOT NULL CHECK (estimated_cost_usd >= 0),
 CHECK ((provider = 'openai' AND service_tier IS NOT NULL) OR (provider = 'anthropic' AND service_tier IS NULL)),
 CHECK (cache_creation_5m_tokens + cache_creation_1h_tokens <= cache_creation_tokens)
);
CREATE INDEX IF NOT EXISTS usage_provider_user_time ON usage_records(provider, username, observed_at_utc);
CREATE INDEX IF NOT EXISTS usage_provider_week ON usage_records(provider, week_start_unix);
`

const summaryColumns = `
 SUM(request_count) AS request_count, SUM(messages_count) AS messages_count,
 SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens,
 SUM(cache_read_tokens) AS cache_read_tokens, SUM(cache_creation_tokens) AS cache_creation_tokens,
 SUM(cache_creation_5m_tokens) AS cache_creation_5m_tokens,
 SUM(cache_creation_1h_tokens) AS cache_creation_1h_tokens,
 SUM(input_tokens + cache_read_tokens + cache_creation_tokens) AS total_input_tokens,
 ROUND(SUM(estimated_cost_usd), 6) AS estimated_cost_usd`

var summaryViews = []string{
	`CREATE VIEW IF NOT EXISTS usage_by_provider AS SELECT provider,` + summaryColumns + ` FROM usage_records GROUP BY provider`,
	`CREATE VIEW IF NOT EXISTS usage_by_user AS SELECT provider, username,` + summaryColumns + ` FROM usage_records GROUP BY provider, username`,
	`CREATE VIEW IF NOT EXISTS usage_by_model AS SELECT provider, model, service_tier, context_window,` + summaryColumns + ` FROM usage_records GROUP BY provider, model, service_tier, context_window`,
	`CREATE VIEW IF NOT EXISTS usage_by_day AS SELECT provider, substr(observed_at_utc,1,10) AS day_utc,` + summaryColumns + ` FROM usage_records GROUP BY provider, day_utc`,
	`CREATE VIEW IF NOT EXISTS usage_by_week AS SELECT provider, username, week_start_unix,` + summaryColumns + ` FROM usage_records WHERE week_start_unix IS NOT NULL GROUP BY provider, username, week_start_unix`,
}
