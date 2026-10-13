package ocm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNeedsRefresh(t *testing.T) {
	fresh, old := time.Now(), time.Now().Add(-9*24*time.Hour)
	for _, tc := range []struct {
		name        string
		credentials oauthCredentials
		want        bool
	}{
		{"api key", oauthCredentials{APIKey: "key"}, false},
		{"no tokens", oauthCredentials{}, false},
		{"no refresh token", oauthCredentials{Tokens: &tokenData{AccessToken: "access"}}, false},
		{"no timestamp", oauthCredentials{Tokens: &tokenData{RefreshToken: "refresh"}}, true},
		{"fresh", oauthCredentials{Tokens: &tokenData{RefreshToken: "refresh"}, LastRefresh: &fresh}, false},
		{"expired", oauthCredentials{Tokens: &tokenData{RefreshToken: "refresh"}, LastRefresh: &old}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.credentials.needsRefresh(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRefreshAndCredentialRoundtrip(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]string
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || request["refresh_token"] != "old" || request["client_id"] != oauth2ClientID || request["grant_type"] != "refresh_token" {
			t.Errorf("unexpected refresh request: %v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","id_token":"new-id"}`))
	}))
	defer endpoint.Close()
	before := time.Now()
	original := &oauthCredentials{Tokens: &tokenData{RefreshToken: "old", AccountID: "account"}}
	credentials, err := refreshToken(context.Background(), endpoint.Client(), original, endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	if original.Tokens.RefreshToken != "old" || original.Tokens.AccessToken != "" {
		t.Fatal("refresh mutated the original credentials snapshot")
	}
	if credentials.Tokens.AccessToken != "new-access" || credentials.Tokens.RefreshToken != "new-refresh" || credentials.Tokens.IDToken != "new-id" || credentials.Tokens.AccountID != "account" || credentials.LastRefresh == nil || credentials.LastRefresh.Before(before) {
		t.Fatalf("bad credentials: %+v", credentials)
	}
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := writeCredentialsToFile(context.Background(), credentials, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := readCredentialsFromFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Tokens == nil || !reflect.DeepEqual(loaded.Tokens, credentials.Tokens) || !loaded.LastRefresh.Equal(*credentials.LastRefresh) {
		t.Fatal("credential roundtrip changed data")
	}
	t.Setenv("CODEX_HOME", filepath.Dir(path))
	defaultPath, err := getDefaultCredentialsPath()
	if err != nil || defaultPath != path {
		t.Fatalf("default path = %q, %v", defaultPath, err)
	}
}
