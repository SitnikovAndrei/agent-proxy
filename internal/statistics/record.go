package statistics

import (
	"database/sql"
	"errors"
	"math"
	"time"
)

type Provider uint8

const (
	OpenAI Provider = iota
	Anthropic
)

func (p Provider) wireValue() (string, error) {
	switch p {
	case OpenAI:
		return "openai", nil
	case Anthropic:
		return "anthropic", nil
	default:
		return "", errors.New("invalid statistics provider")
	}
}

// InputTokens excludes cache reads and cache writes for both providers.
type Counters struct {
	Requests, Messages                                                int64
	InputTokens, OutputTokens, CacheReadTokens                        int64
	CacheCreationTokens, CacheCreation5mTokens, CacheCreation1hTokens int64
}

func (c Counters) values() []int64 {
	return []int64{c.Requests, c.Messages, c.InputTokens, c.OutputTokens, c.CacheReadTokens,
		c.CacheCreationTokens, c.CacheCreation5mTokens, c.CacheCreation1hTokens}
}

func (c Counters) Validate() error {
	for _, value := range c.values() {
		if value < 0 {
			return errors.New("usage counters must be nonnegative")
		}
	}
	if c.CacheCreation5mTokens > c.CacheCreationTokens || c.CacheCreation1hTokens > c.CacheCreationTokens-c.CacheCreation5mTokens {
		return errors.New("cache creation TTL counters exceed total cache creation")
	}
	return nil
}

type Record struct {
	ObservedAt       time.Time
	Provider         Provider
	Model            string
	Tier             sql.Null[ServiceTier]
	ContextWindow    int
	Username         sql.Null[string]
	WeekStart        sql.Null[int64]
	Counters         Counters
	EstimatedCostUSD float64
}

func (r Record) Validate() error {
	if r.ObservedAt.IsZero() || r.Counters.Requests != 1 {
		return errors.New("usage requires observation time and exactly one request")
	}
	if _, err := r.Provider.wireValue(); err != nil {
		return err
	}
	if r.Model == "" || r.ContextWindow <= 0 || (r.Username.Valid && r.Username.V == "") {
		return errors.New("usage requires model, positive context window and nonempty username when present")
	}
	if r.Tier.Valid != (r.Provider == OpenAI) {
		return errors.New("service tier is required only for OpenAI usage")
	}
	if r.Tier.Valid {
		if _, err := r.Tier.V.WireValue(); err != nil {
			return err
		}
	}
	if r.WeekStart.Valid && r.WeekStart.V <= 0 {
		return errors.New("invalid usage timestamp")
	}
	if math.IsNaN(r.EstimatedCostUSD) || math.IsInf(r.EstimatedCostUSD, 0) || r.EstimatedCostUSD < 0 {
		return errors.New("estimated usage cost must be finite and nonnegative")
	}
	return r.Counters.Validate()
}
