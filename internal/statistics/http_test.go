package statistics

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"testing"

	"ocm/internal/config"
	"ocm/internal/proxy"
)

func TestParseFilterErrors(t *testing.T) {
	for _, query := range []string{"unknown=x", "model=", "model=a&model=b", "from=bad", "to=bad", "from=2026-01-03&to=2026-01-02", "limit=0", "limit=501", "limit=bad", "offset=-1", "offset=1000001"} {
		t.Run(query, func(t *testing.T) {
			values, err := url.ParseQuery(query)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseFilter(values, "alice", OpenAI); err == nil {
				t.Fatal("invalid filter accepted")
			}
		})
	}
	if _, err := parseFilter(url.Values{"user": {"bob"}}, "alice", OpenAI); !errors.Is(err, errUserScope) {
		t.Fatal(err)
	}
	values := url.Values{"model": {"model-a"}, "from": {"2026-01-01"}, "to": {"2026-01-02T00:00:00Z"}, "limit": {"5"}, "offset": {"2"}, "user": {"alice"}}
	filter, err := parseFilter(values, "alice", OpenAI)
	if err != nil || filter.Model.V != "model-a" || !filter.From.Valid || !filter.To.Valid || filter.Limit != 5 || filter.Offset != 2 {
		t.Fatalf("filter=%+v err=%v", filter, err)
	}
}

func TestHTTPStatisticsAuthorization(t *testing.T) {
	store := testStore(t)
	if err := store.Record(validRecord()); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		users       []config.User
		token, path string
		status      int
	}{
		{name: "empty users", token: "synthetic", path: "/stats", status: 401},
		{name: "missing bearer", users: []config.User{{Name: "alice", Token: "alice-token"}}, path: "/stats", status: 401},
		{name: "duplicate names", users: []config.User{{Name: "alice", Token: "a"}, {Name: "alice", Token: "b"}}, token: "a", path: "/stats", status: 403},
		{name: "empty name", users: []config.User{{Token: "a"}}, token: "a", path: "/stats", status: 403},
		{name: "other user", users: []config.User{{Name: "alice", Token: "a"}, {Name: "bob", Token: "b"}}, token: "a", path: "/stats?user=bob", status: 403},
		{name: "own scope", users: []config.User{{Name: "alice", Token: "a"}}, token: "a", path: "/stats?user=alice", status: 200},
		{name: "bob scope", users: []config.User{{Name: "bob", Token: "b"}}, token: "b", path: "/stats", status: 200},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			users := &proxy.UserManager{}
			users.UpdateUsers(test.users)
			handler, err := NewHandler(store, users, OpenAI, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("GET", test.path, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			writer := httptest.NewRecorder()
			handler.ServeHTTP(writer, request)
			if writer.Code != test.status {
				t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
			}
			if test.status == 200 {
				var summary Summary
				if err := json.Unmarshal(writer.Body.Bytes(), &summary); err != nil {
					t.Fatal(err)
				}
				expected := int64(1)
				if test.name == "bob scope" {
					expected = 0
				}
				if summary.Totals.Requests != expected {
					t.Fatalf("summary=%+v", summary)
				}
			}
		})
	}
}
