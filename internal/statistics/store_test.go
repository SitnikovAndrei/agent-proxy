package statistics

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func validRecord() Record {
	return Record{ObservedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Provider: OpenAI, Model: "model-a", Tier: sql.Null[ServiceTier]{V: TierDefault, Valid: true}, ContextWindow: 128000, Username: sql.Null[string]{V: "alice", Valid: true}, Counters: Counters{Requests: 1, Messages: 2, InputTokens: 10, OutputTokens: 5, CacheReadTokens: 3, CacheCreationTokens: 4, CacheCreation5mTokens: 4}, EstimatedCostUSD: 0.01}
}

func TestRecordValidation(t *testing.T) {
	if err := validRecord().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*Record)
	}{
		{"time", func(r *Record) { r.ObservedAt = time.Time{} }},
		{"requests", func(r *Record) { r.Counters.Requests = 2 }},
		{"provider", func(r *Record) { r.Provider = Provider(99) }},
		{"model", func(r *Record) { r.Model = "" }},
		{"context", func(r *Record) { r.ContextWindow = 0 }},
		{"username", func(r *Record) { r.Username.V = "" }},
		{"tier missing", func(r *Record) { r.Tier.Valid = false }},
		{"tier invalid", func(r *Record) { r.Tier.V = ServiceTier(99) }},
		{"anthropic tier", func(r *Record) { r.Provider = Anthropic }},
		{"week", func(r *Record) { r.WeekStart = sql.Null[int64]{Valid: true} }},
		{"negative", func(r *Record) { r.Counters.OutputTokens = -1 }},
		{"cache ttl", func(r *Record) { r.Counters.CacheCreation1hTokens = 1 }},
		{"cost", func(r *Record) { r.EstimatedCostUSD = -1 }},
		{"nan", func(r *Record) { r.EstimatedCostUSD = math.NaN() }},
		{"infinite", func(r *Record) { r.EstimatedCostUSD = math.Inf(1) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			r := validRecord()
			test.change(&r)
			if err := r.Validate(); err == nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func TestRecordAndSummaryFilters(t *testing.T) {
	store := testStore(t)
	for _, model := range []string{"model-a", "model-b"} {
		for _, user := range []string{"alice", "bob"} {
			for _, day := range []int{1, 2, 3} {
				r := validRecord()
				r.Model = model
				r.Username.V = user
				r.ObservedAt = time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC)
				if err := store.Record(r); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// The same username in another provider must not cross the scope boundary.
	r := validRecord()
	r.Provider = Anthropic
	r.Tier.Valid = false
	if err := store.Record(r); err != nil {
		t.Fatal(err)
	}
	filter := Filter{Provider: OpenAI, Username: "alice", Limit: 1}
	summary, err := store.Summary(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Totals.Requests != 6 || summary.Totals.InputTokens != 60 || summary.Totals.TotalInputTokens != 102 || !summary.HasMore || len(summary.ByModel) != 1 || summary.ByModel[0].Model != "model-a" {
		t.Fatalf("summary=%+v", summary)
	}
	filter.Offset = 1
	page, err := store.Summary(context.Background(), filter)
	if err != nil || page.HasMore || len(page.ByModel) != 1 || page.ByModel[0].Model != "model-b" || page.Totals != summary.Totals {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	filter.Offset = 2
	page, err = store.Summary(context.Background(), filter)
	if err != nil || len(page.ByModel) != 0 || page.HasMore {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	filter.Offset = 0
	filter.Model = sql.Null[string]{V: "model-a", Valid: true}
	filter.From = sql.Null[time.Time]{V: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Valid: true}
	filter.To = sql.Null[time.Time]{V: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Valid: true}
	summary, err = store.Summary(context.Background(), filter)
	if err != nil || summary.Totals.Requests != 1 || summary.HasMore || len(summary.ByModel) != 1 {
		t.Fatalf("filtered=%+v err=%v", summary, err)
	}
	filter.Username = "nobody"
	summary, err = store.Summary(context.Background(), filter)
	if err != nil || summary.Totals.Requests != 0 || len(summary.ByModel) != 0 {
		t.Fatalf("scope=%+v err=%v", summary, err)
	}
	invalid := validRecord()
	invalid.Model = ""
	if err := store.Record(invalid); err == nil {
		t.Fatal("store accepted invalid record")
	}
}
