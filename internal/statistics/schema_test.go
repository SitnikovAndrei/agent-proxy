package statistics

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

const timeBoundIndex = "CREATE INDEX IF NOT EXISTS usage_provider_user_time_bound ON usage_records(provider, username, replace(observed_at_utc,'Z',''));"

func TestTimeBoundIndex(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, query := range []string{
		"SELECT " + totalsColumns + filteredUsage,
		"SELECT * FROM usage_records WHERE provider = :provider AND username = :user AND replace(observed_at_utc,'Z','') >= :from_time AND replace(observed_at_utc,'Z','') < :to_time",
	} {
		rows, err := store.db.Query("EXPLAIN QUERY PLAN "+query,
			sql.Named("provider", "openai"), sql.Named("user", "test"), sql.Named("model", nil),
			sql.Named("from_time", "2026-01-01"), sql.Named("to_time", "2026-02-01"))
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if !strings.Contains(strings.Join(plan, "\n"), "usage_provider_user_time_bound") {
			t.Fatalf("index not used: %v", plan)
		}
	}
}

func TestOpenOldSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	oldSchema := strings.Replace(schema, timeBoundIndex, "", 1)
	if oldSchema == schema {
		t.Fatal("old schema must exclude new index")
	}
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	// An existing row must survive initialization, including its timestamp and cost.
	if _, err := db.Exec(`INSERT INTO usage_records VALUES(1,'openai','gpt-5.4','default',272000,'test','2026-01-01T00:00:00Z',NULL,1,0,100,20,0,0,0,0,0.123)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		store, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var count, synchronous int
		var observed string
		var cost float64
		if err := store.db.QueryRow("SELECT count(*), observed_at_utc, estimated_cost_usd FROM usage_records").Scan(&count, &observed, &cost); err != nil {
			t.Fatal(err)
		}
		if count != 1 || observed != "2026-01-01T00:00:00Z" || cost != 0.123 {
			t.Fatalf("existing row changed: %d %s %v", count, observed, cost)
		}
		if err := store.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='usage_provider_user_time_bound'").Scan(&count); err != nil || count != 1 {
			t.Fatalf("index missing: %d %v", count, err)
		}
		if err := store.db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil || synchronous != 2 {
			t.Fatalf("synchronous = %d, %v", synchronous, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
