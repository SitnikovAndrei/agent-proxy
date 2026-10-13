package ocm

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ocm/internal/statistics"
)

func TestWeeklyCycleHint(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers http.Header
		window  int64
		reset   int64
	}{
		{"missing", http.Header{}, 0, 0},
		{"fallback", http.Header{"X-Codex-Secondary-Window-Minutes": []string{"10080"}, "X-Codex-Secondary-Reset-At": []string{"1800000000"}}, 10080, 1800000000},
		{"active", http.Header{"X-Codex-Active-Limit": []string{" GPT_5 "}, "X-Gpt-5-Secondary-Window-Minutes": []string{"10080"}, "X-Gpt-5-Secondary-Reset-At": []string{"1800000001"}}, 10080, 1800000001},
		{"invalid", http.Header{"X-Codex-Secondary-Window-Minutes": []string{"bad"}, "X-Codex-Secondary-Reset-At": []string{"1800000000"}}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hint := extractWeeklyCycleHint(tc.headers)
			if tc.window == 0 {
				if hint != nil {
					t.Fatalf("unexpected hint: %+v", hint)
				}
				return
			}
			if hint == nil || hint.WindowMinutes != tc.window || hint.ResetAt.Unix() != tc.reset {
				t.Fatalf("bad hint: %+v", hint)
			}
		})
	}
}

func TestUsageSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	store, err := statistics.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tracker := &usageTracker{store: store}
	cycle := &WeeklyCycleHint{WindowMinutes: 10080, ResetAt: time.Unix(1799395200, 0).Add(7 * 24 * time.Hour)}
	for range 2 {
		if err := tracker.AddUsageWithCycleHint("gpt-5.4", contextWindowStandard, 500000, 10000, 1000, statistics.TierDefault, "tester", time.Now(), cycle); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertStoredUsage(t, db)
}

func TestConcurrentUsageSavesAndShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	store, err := statistics.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	usage := &usageTracker{store: store}
	var workers sync.WaitGroup
	for range 10 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := usage.AddUsageWithCycleHint("gpt-5.4", contextWindowStandard, 100, 20, 10, statistics.TierDefault, "tester", time.Now(), nil); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, requests, input, output, cached int64
	err = db.QueryRow(`SELECT COUNT(*),SUM(request_count),SUM(input_tokens+cache_read_tokens),SUM(output_tokens),SUM(cache_read_tokens) FROM usage_records`).Scan(&count, &requests, &input, &output, &cached)
	if err != nil {
		t.Fatal(err)
	}
	if count != 10 || requests != 10 || input != 1000 || output != 200 || cached != 100 {
		t.Fatal("concurrent writes lost usage")
	}
}

func assertStoredUsage(t *testing.T, db *sql.DB) {
	t.Helper()
	var model, user, tier string
	var window int
	var requests, input, output, cached, week int64
	var cost float64
	err := db.QueryRow(`SELECT model,username,service_tier,context_window,SUM(request_count),SUM(input_tokens+cache_read_tokens),
 SUM(output_tokens),SUM(cache_read_tokens),week_start_unix,SUM(estimated_cost_usd) FROM usage_records
 GROUP BY model,username,service_tier,context_window,week_start_unix`).Scan(
		&model, &user, &tier, &window, &requests, &input, &output, &cached, &week, &cost)
	if err != nil {
		t.Fatal(err)
	}
	if model != "gpt-5.4" || user != "tester" || tier != "default" || window != contextWindowStandard || requests != 2 || input != 1000000 || output != 20000 || cached != 2000 || week != 1799395200 || cost <= 0 {
		t.Fatal("usage roundtrip changed data or cost was not calculated")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM usage_records").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("incorrect stored record count: %d rows", count)
	}
}
