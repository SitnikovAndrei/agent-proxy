package ocm

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ocm/internal/config"
	"ocm/internal/statistics"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const completedEvent = `{"type":"response.completed","response":{"id":"resp_test","model":"gpt-5.4","service_tier":"default","usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":10}}}}`

func startTestService(t *testing.T, upstream string, oauth bool, logs io.Writer) *Service {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	credentials := &oauthCredentials{APIKey: "upstream-key"}
	if oauth {
		credentials = &oauthCredentials{Tokens: &tokenData{AccessToken: "access-token", RefreshToken: "refresh-token", AccountID: "account-id"}, LastRefresh: &now}
	}
	path := filepath.Join(dir, "auth.json")
	if err := writeCredentialsToFile(context.Background(), credentials, path); err != nil {
		t.Fatal(err)
	}
	store, err := statistics.Open(filepath.Join(dir, "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	service, err := NewService(Options{Config: config.Config{
		Listen: "127.0.0.1", CredentialPath: path,
		Users: []config.User{{Name: "tester", Token: "client-key"}}, Headers: config.Headers{"X-Custom": []string{"configured"}},
	}, openaiAPIBaseURL: upstream, chatGPTBackendURL: upstream}, slog.New(slog.NewTextHandler(logs, nil)), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	return service
}

func requestService(t *testing.T, service *Service, path, token string) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequest("POST", "http://"+service.listener.Addr().String()+path, strings.NewReader(`{"model":"gpt-5.4"}`))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", token)
	}
	request.Header.Set("X-Custom", "client")
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, data
}

func TestHTTPAuthentication(t *testing.T) {
	var logs bytes.Buffer
	service := startTestService(t, "http://127.0.0.1:1", false, &logs)
	for _, token := range []string{"", "invalid", "Bearer wrong-secret-token"} {
		status, headers, body := requestService(t, service, "/v1/responses", token)
		var response errorResponse
		if status != http.StatusUnauthorized || headers.Get("Content-Type") != "application/json" || json.Unmarshal(body, &response) != nil || response.Error.Type != "authentication_error" {
			t.Fatalf("unexpected auth response: %d %s", status, body)
		}
	}
	if strings.Contains(logs.String(), "wrong-secret-token") || strings.Contains(logs.String(), "wron") || !strings.Contains(logs.String(), "authentication failed") {
		t.Fatalf("unsafe or missing authentication log: %s", logs.String())
	}
}

func TestHTTPRoutingAndSSEUsage(t *testing.T) {
	for _, oauth := range []bool{true, false} {
		name := "api-key"
		if oauth {
			name = "oauth"
		}
		t.Run(name, func(t *testing.T) {
			stream := "event: response.completed\ndata: " + completedEvent + "\n\ndata: [DONE]\n\n"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath, expectedToken := "/v1/responses", "Bearer upstream-key"
				if oauth {
					expectedPath, expectedToken = "/responses", "Bearer access-token"
				}
				if r.URL.Path != expectedPath || r.URL.RawQuery != "test=1" || r.Header.Get("Authorization") != expectedToken || r.Header.Get("X-Custom") != "configured" {
					t.Errorf("incorrect upstream request: %s %v", r.URL, r.Header)
				}
				if oauth && r.Header.Get("ChatGPT-Account-Id") != "account-id" {
					t.Error("missing account ID")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				for _, part := range []string{stream[:25], stream[25:]} {
					_, _ = io.WriteString(w, part)
					w.(http.Flusher).Flush()
				}
			}))
			defer upstream.Close()
			service := startTestService(t, upstream.URL, oauth, io.Discard)
			if oauth {
				status, _, _ := requestService(t, service, "/v1/chat/completions", "Bearer client-key")
				if status != http.StatusBadRequest {
					t.Fatalf("chat status: %d", status)
				}
			}
			status, _, body := requestService(t, service, "/v1/responses?test=1", "Bearer client-key")
			if status != http.StatusOK || string(body) != stream {
				t.Fatalf("stream changed: %d %q", status, body)
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			assertSavedUsage(t, testUsagePath(service))
		})
	}
}

func testUsagePath(service *Service) string {
	return filepath.Join(filepath.Dir(service.options.CredentialPath), "usage.sqlite")
}

func assertSavedUsage(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, requests, input, output, cached int64
	var model, user string
	err = db.QueryRow(`SELECT COUNT(*), SUM(request_count), SUM(input_tokens + cache_read_tokens),
 SUM(output_tokens), SUM(cache_read_tokens), MIN(model), MIN(username) FROM usage_records`).Scan(&count, &requests, &input, &output, &cached, &model, &user)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || model != "gpt-5.4" || user != "tester" || requests != 1 || input != 100 || output != 20 || cached != 10 {
		t.Fatalf("incorrect usage: count=%d model=%s user=%s requests=%d input=%d output=%d cached=%d", count, model, user, requests, input, output, cached)
	}
}

func TestWebSocketProxyAndUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer access-token" || r.Header.Get("ChatGPT-Account-Id") != "account-id" || r.Header.Get("X-Custom") != "configured" {
			t.Errorf("incorrect websocket request: %s %v", r.URL, r.Header)
		}
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		data, op, err := wsutil.ReadClientData(conn)
		if err != nil {
			t.Error(err)
			return
		}
		if op != ws.OpText || !bytes.Contains(data, []byte("response.create")) {
			t.Errorf("unexpected message: %s", data)
			return
		}
		if err := wsutil.WriteServerMessage(conn, ws.OpText, []byte(completedEvent)); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = wsutil.ReadClientData(conn)
	}))
	defer upstream.Close()
	service := startTestService(t, upstream.URL, true, io.Discard)
	dialer := ws.Dialer{Header: ws.HandshakeHeaderHTTP(http.Header{"Authorization": []string{"Bearer client-key"}})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, buffered, _, err := dialer.Dial(ctx, "ws://"+service.listener.Addr().String()+"/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := wsutil.WriteClientMessage(conn, ws.OpText, []byte(`{"type":"response.create","model":"gpt-5.4"}`)); err != nil {
		t.Fatal(err)
	}
	var rw io.ReadWriter = conn
	if buffered != nil {
		rw = struct {
			io.Reader
			io.Writer
		}{buffered, conn}
	}
	data, op, err := wsutil.ReadServerData(rw)
	if err != nil || op != ws.OpText || string(data) != completedEvent {
		t.Fatalf("event = %s, op = %v, err = %v", data, op, err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	assertSavedUsage(t, testUsagePath(service))
}
