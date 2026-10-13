package ocm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func TestHTTPCredentialReload(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path+"|"+r.Header.Get("Authorization")+"|"+r.Header.Get("ChatGPT-Account-Id"))
	}))
	defer upstream.Close()
	service := startTestService(t, upstream.URL, false, io.Discard)
	now := time.Now()
	cases := []struct {
		credentials *oauthCredentials
		want        string
	}{
		{&oauthCredentials{APIKey: "replacement-key"}, "/v1/responses|Bearer replacement-key|"},
		{&oauthCredentials{Tokens: &tokenData{AccessToken: "oauth-access", AccountID: "new-account"}, LastRefresh: &now}, "/responses|Bearer oauth-access|new-account"},
	}
	for _, tc := range cases {
		if err := writeCredentialsToFile(context.Background(), tc.credentials, service.credentialStore.path); err != nil {
			t.Fatal(err)
		}
		status, _, body := requestService(t, service, "/v1/responses", "Bearer client-key")
		if status != http.StatusOK || string(body) != tc.want {
			t.Fatalf("credentials, routing or account ID stale: %d %q", status, body)
		}
	}
}

func TestWebSocketCredentialReload(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer replacement-access" || r.Header.Get("ChatGPT-Account-Id") != "replacement-account" {
			t.Error("websocket used stale credentials or routing")
		}
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		if err := wsutil.WriteServerMessage(conn, ws.OpText, []byte(completedEvent)); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	service := startTestService(t, upstream.URL, false, io.Discard)
	credentials := &oauthCredentials{Tokens: &tokenData{AccessToken: "replacement-access", AccountID: "replacement-account"}}
	if err := writeCredentialsToFile(context.Background(), credentials, service.credentialStore.path); err != nil {
		t.Fatal(err)
	}
	dialer := ws.Dialer{Header: ws.HandshakeHeaderHTTP(http.Header{"Authorization": []string{"Bearer client-key"}})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, reader, _, err := dialer.Dial(ctx, "ws://"+service.listener.Addr().String()+"/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	var source io.ReadWriter = conn
	if reader != nil {
		source = struct {
			io.Reader
			io.Writer
		}{reader, conn}
	}
	data, _, err := wsutil.ReadServerData(source)
	if err != nil || string(data) != completedEvent {
		t.Fatalf("unexpected websocket event: %s, %v", data, err)
	}
}

func TestAuthenticationDoesNotWaitForRefresh(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer endpoint.Close()
	defer close(release)
	service := startTestService(t, "http://unused", true, io.Discard)
	service.credentialStore.tokenURL = endpoint.URL
	credentials := &oauthCredentials{Tokens: &tokenData{RefreshToken: "old-refresh"}}
	if err := writeCredentialsToFile(context.Background(), credentials, service.credentialStore.path); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer client-key")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() { service.ServeHTTP(httptest.NewRecorder(), request.WithContext(ctx)); close(finished) }()
	awaitSignal(t, entered)
	status, _, _ := requestService(t, service, "/v1/responses", "Bearer wrong-key")
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthorized request returned %d", status)
	}
	cancel()
	awaitSignal(t, finished)
}
