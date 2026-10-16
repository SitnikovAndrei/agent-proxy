package ccm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshPreservesCredentialDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	initial := `{"root_unknown":{"keep":[1,2]},"claudeAiOauth":{"accessToken":"old-access","refreshToken":"old-refresh","expiresAt":1,"scopes":["scope"],"subscriptionType":"max","isMax":true,"nested_unknown":{"keep":true}}}`
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			GrantType    string `json:"grant_type"`
			RefreshToken string `json:"refresh_token"`
			ClientID     string `json:"client_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || payload.GrantType != "refresh_token" || payload.RefreshToken != "old-refresh" || payload.ClientID != oauth2ClientID {
			t.Error("unexpected refresh request")
		}
		// Simulate Claude Code updating unrelated fields while the request is in flight.
		changed := `{"root_unknown":{"keep":[3,4]},"claudeAiOauth":{"accessToken":"old-access","refreshToken":"old-refresh","expiresAt":1,"scopes":["scope"],"subscriptionType":"max","isMax":true,"nested_unknown":{"keep":false}}}`
		if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
	}))
	defer server.Close()
	client := server.Client()
	defer client.CloseIdleConnections()
	store := credentialStore{path: path, client: client, tokenURL: server.URL, timeout: time.Second}
	got, err := store.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-access" || got.RefreshToken != "new-refresh" || got.needsRefresh() || got.SubscriptionType != "max" || !got.IsMax || len(got.Scopes) != 1 {
		t.Fatalf("refreshed metadata incorrect: %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Root struct {
			Keep []int `json:"keep"`
		} `json:"root_unknown"`
		OAuth struct {
			Nested struct {
				Keep bool `json:"keep"`
			} `json:"nested_unknown"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Root.Keep) != 2 || document.Root.Keep[0] != 3 || document.OAuth.Nested.Keep {
		t.Fatalf("unknown fields lost: %s", data)
	}
	again, err := store.load(context.Background())
	if err != nil || again.AccessToken != got.AccessToken || calls.Load() != 1 {
		t.Fatalf("unneeded refresh calls=%d err=%v", calls.Load(), err)
	}
}
