package ccm

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ocm/internal/proxy"
)

func requestTestService(t *testing.T, endpoint string) *Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","refreshToken":"synthetic-refresh","expiresAt":0}}`), 0600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	lifecycle := proxy.NewLifecycle(ctx, cancel, slog.Default(), client, context.DeadlineExceeded)
	return &Service{ctx: ctx, lifecycle: lifecycle, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), users: &proxy.UserManager{},
		credentials: &credentialStore{path: path}, client: client, baseURL: endpoint}
}

type repeatingBody struct{}

func (repeatingBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestOversizedRequestRejected(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer upstream.Close()
			service := requestTestService(t, upstream.URL)
			request := httptest.NewRequest("POST", path, io.LimitReader(repeatingBody{}, proxy.MaxRequestBodyBytes+1))
			writer := httptest.NewRecorder()
			service.ServeHTTP(writer, request)
			if writer.Code != http.StatusRequestEntityTooLarge || !strings.Contains(writer.Body.String(), `"type":"request_too_large"`) || !strings.Contains(writer.Body.String(), `"message":"request body too large"`) || calls.Load() != 0 {
				t.Fatalf("status=%d body=%s calls=%d", writer.Code, writer.Body.String(), calls.Load())
			}

		})
	}
}

func TestRequestContentLengthWithoutTracking(t *testing.T) {
	payload := `{"model":"claude-test","messages":[]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != payload || r.ContentLength != int64(len(payload)) || len(r.TransferEncoding) != 0 {
			t.Errorf("body=%q length=%d transfer=%v error=%v", body, r.ContentLength, r.TransferEncoding, err)
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-access" {
			t.Error("upstream credential missing")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	service := requestTestService(t, upstream.URL)
	writer := httptest.NewRecorder()
	service.ServeHTTP(writer, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(payload)))
	if writer.Code != http.StatusNoContent {
		t.Fatalf("status = %d", writer.Code)
	}
	request, err := service.buildProxyRequest(httptest.NewRequest("GET", "/v1/models", nil), oauthCredentials{AccessToken: "synthetic"}, nil)
	if err != nil || request.Body != http.NoBody {
		t.Fatalf("empty body: %+v %v", request, err)
	}
}

func TestBufferedRequestGetBody(t *testing.T) {
	service := &Service{baseURL: "http://localhost"}
	request, err := service.buildProxyRequest(httptest.NewRequest("POST", "/v1/messages", nil), oauthCredentials{AccessToken: "synthetic"}, []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	if request.ContentLength != 4 || request.GetBody == nil {
		t.Fatalf("length=%d GetBody missing=%v", request.ContentLength, request.GetBody == nil)
	}
	replay, err := request.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	data, err := io.ReadAll(replay)
	if err != nil || string(data) != "body" {
		t.Fatalf("replay=%q err=%v", data, err)
	}
}

func TestLargeUploadStreamsToUpstream(t *testing.T) {
	const size = proxy.MaxRequestBodyBytes + 1024
	for _, path := range []string{"/v1/files", "/v1/messages-extra"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.ContentLength != size || len(r.TransferEncoding) != 0 {
					t.Errorf("length=%d transfer=%v", r.ContentLength, r.TransferEncoding)
				}
				received, err := io.Copy(io.Discard, r.Body)
				if err != nil || received != size {
					t.Errorf("received=%d err=%v", received, err)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer upstream.Close()
			service := requestTestService(t, upstream.URL)
			request := httptest.NewRequest("POST", path, io.LimitReader(repeatingBody{}, size))
			request.ContentLength = size

			writer := httptest.NewRecorder()
			service.ServeHTTP(writer, request)
			if writer.Code != http.StatusNoContent || calls.Load() != 1 {
				t.Fatalf("status=%d calls=%d body=%s", writer.Code, calls.Load(), writer.Body.String())
			}
		})
	}
}

type unreadBody struct{ reads int }

func (b *unreadBody) Read([]byte) (int, error) { b.reads++; return 0, io.ErrUnexpectedEOF }
func (*unreadBody) Close() error               { return nil }

func TestUninspectedRequestBodyPreserved(t *testing.T) {
	service := &Service{baseURL: "http://localhost"}
	for _, length := range []int64{-1, 0, 123} {
		incoming := httptest.NewRequest("POST", "/v1/files", nil)
		body := &unreadBody{}
		incoming.Body = body
		incoming.ContentLength = length
		_, payload, err := service.readRequestMetadata(httptest.NewRecorder(), incoming)
		if err != nil || body.reads != 0 {
			t.Fatalf("preparation read the body: reads=%d err=%v", body.reads, err)
		}
		request, err := service.buildProxyRequest(incoming, oauthCredentials{AccessToken: "synthetic"}, payload)
		if err != nil {
			t.Fatal(err)
		}
		if body.reads != 0 || request.ContentLength != length {
			t.Fatalf("reads=%d length=%d want=%d", body.reads, request.ContentLength, length)
		}
		if length == 0 {
			if request.Body != http.NoBody {
				t.Fatal("zero length body must be http.NoBody")
			}
		} else if request.Body != body || request.GetBody != nil {
			t.Fatal("body must be forwarded directly without buffering or replay")
		}
	}
}

func TestInspectedBodyReadError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected upstream request") }))
	defer upstream.Close()
	service := requestTestService(t, upstream.URL)
	request := httptest.NewRequest("POST", "/v1/messages", nil)
	request.Body = &unreadBody{}
	request.ContentLength = -1

	writer := httptest.NewRecorder()
	service.ServeHTTP(writer, request)
	if writer.Code != http.StatusBadRequest || !strings.Contains(writer.Body.String(), `"type":"invalid_request_error"`) || !strings.Contains(writer.Body.String(), `"message":"cannot read request body"`) {
		t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
	}
}

func TestStreamingPathDoesNotReadBodyBeforeCredentials(t *testing.T) {
	service := requestTestService(t, "http://unused")
	service.credentials.path += ".missing"
	request := httptest.NewRequest("POST", "/v1/files", nil)
	body := &unreadBody{}
	request.Body = body
	request.ContentLength = -1
	writer := httptest.NewRecorder()
	service.ServeHTTP(writer, request)
	if writer.Code != http.StatusUnauthorized || body.reads != 0 {
		t.Fatalf("status=%d reads=%d", writer.Code, body.reads)
	}
}
