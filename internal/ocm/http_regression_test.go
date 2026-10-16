package ocm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"ocm/internal/proxy"
)

type repeatingBody struct{}

func (repeatingBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestOversizedRequestRejected(t *testing.T) {
	for _, tc := range []struct {
		name, path      string
		oauth, tracking bool
	}{
		{"API key", "/v1/responses", false, true},
		{"without tracking", "/v1/responses", false, false},
		{"responses subpath", "/v1/responses/resp_test/input_items", false, true},
		{"API key chat", "/v1/chat/completions", false, true},
		{"chat adapter", "/v1/chat/completions", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer upstream.Close()
			service := startTestService(t, upstream.URL, tc.oauth, io.Discard)
			if tc.oauth {
				credentials := &oauthCredentials{Tokens: &tokenData{AccessToken: "old-access", RefreshToken: "old-refresh"}}
				if err := writeCredentialsToFile(context.Background(), credentials, service.credentialStore.path); err != nil {
					t.Fatal(err)
				}
				service.credentialStore.tokenURL = upstream.URL
			}
			if !tc.tracking {
				service.usageTracker = nil
			}
			request := httptest.NewRequest("POST", tc.path, io.LimitReader(repeatingBody{}, proxy.MaxRequestBodyBytes+1))
			request.Header.Set("Authorization", "Bearer client-key")
			writer := httptest.NewRecorder()
			service.ServeHTTP(writer, request)
			if writer.Code != http.StatusRequestEntityTooLarge || !strings.Contains(writer.Body.String(), `"type":"invalid_request_error"`) || !strings.Contains(writer.Body.String(), `"message":"request body too large"`) || calls.Load() != 0 {
				t.Fatalf("status=%d body=%s calls=%d", writer.Code, writer.Body.String(), calls.Load())
			}
		})
	}
}

func TestHTTPEncodingLengthAndIdentity(t *testing.T) {
	for _, configured := range []bool{false, true} {
		for _, tracking := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "default", true: "configured"}[configured], map[bool]string{false: "untracked", true: "tracked"}[tracking]}, "/"), func(t *testing.T) {
				payload := `{"model":"gpt-5.4"}`
				agent, origin := codexUserAgent, codexOriginator
				if configured {
					agent, origin = "configured-agent", "configured-origin"
				}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Accept-Encoding") != "gzip" {
						t.Errorf("encoding = %q", r.Header.Get("Accept-Encoding"))
					}
					if r.Header.Get("User-Agent") != agent || r.Header.Get("Originator") != origin {
						t.Errorf("identity = %q %q", r.Header.Get("User-Agent"), r.Header.Get("Originator"))
					}
					body, err := io.ReadAll(r.Body)
					if err != nil || string(body) != payload || r.ContentLength != int64(len(payload)) || len(r.TransferEncoding) != 0 {
						t.Errorf("body=%q length=%d transfer=%v error=%v", body, r.ContentLength, r.TransferEncoding, err)
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				defer upstream.Close()
				service := startTestService(t, upstream.URL, false, io.Discard)
				if !tracking {
					service.usageTracker = nil
				}
				if configured {
					service.httpHeaders.Set("User-Agent", agent)
					service.httpHeaders.Set("Originator", origin)
				}
				request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(payload))
				request.Header.Set("Authorization", "Bearer client-key")
				request.Header.Set("Accept-Encoding", "br")
				request.Header.Set("User-Agent", "client-agent")
				request.Header.Set("Originator", "client-origin")
				writer := httptest.NewRecorder()
				service.ServeHTTP(writer, request)
				if writer.Code != http.StatusNoContent {
					t.Fatalf("status = %d", writer.Code)
				}
			})
		}
	}
}

func TestBufferedRequestMetadata(t *testing.T) {
	service := &Service{openaiAPIBaseURL: "http://localhost"}
	for _, body := range []string{"", `{"model":"gpt-5.4"}`} {
		request, err := service.buildProxyRequest(httptest.NewRequest("POST", "/v1/responses", nil), "/v1/responses", &oauthCredentials{APIKey: "synthetic"}, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if request.ContentLength != int64(len(body)) {
			t.Fatalf("length = %d", request.ContentLength)
		}
		if body == "" {
			if request.Body != http.NoBody {
				t.Fatal("empty body must be http.NoBody")
			}
		} else {
			if request.GetBody == nil {
				t.Fatal("GetBody missing")
			}
			replay, err := request.GetBody()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(replay)
			replay.Close()
			if err != nil || string(data) != body {
				t.Fatalf("replay=%q err=%v", data, err)
			}
		}
	}
}

func TestModelsBodyPreserved(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil || string(data) != "body" || r.ContentLength != 4 || r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("data=%q length=%d err=%v", data, r.ContentLength, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	defer upstream.Close()
	service := startTestService(t, upstream.URL, true, io.Discard)
	request := httptest.NewRequest("GET", "/v1/models", strings.NewReader("body"))
	request.Header.Set("Authorization", "Bearer client-key")
	writer := httptest.NewRecorder()
	service.ServeHTTP(writer, request)
	if writer.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
	}
}

func TestLargeUploadStreamsToUpstream(t *testing.T) {
	const size = proxy.MaxRequestBodyBytes + 1024
	for _, path := range []string{"/v1/files", "/v1/uploads/upload_test/parts", "/v1/responses-extra"} {
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
			service := startTestService(t, upstream.URL, false, io.Discard)
			request := httptest.NewRequest("POST", path, io.LimitReader(repeatingBody{}, size))
			request.ContentLength = size
			request.Header.Set("Authorization", "Bearer client-key")
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
	service := &Service{openaiAPIBaseURL: "http://localhost"}
	for _, length := range []int64{-1, 0, 123} {
		incoming := httptest.NewRequest("POST", "/v1/files", nil)
		body := &unreadBody{}
		incoming.Body = body
		incoming.ContentLength = length
		payload, err := service.readRequestModel(httptest.NewRecorder(), incoming)
		if err != nil || body.reads != 0 {
			t.Fatalf("preparation read the body: reads=%d err=%v", body.reads, err)
		}
		request, err := service.buildProxyRequest(incoming, incoming.URL.Path, &oauthCredentials{APIKey: "synthetic"}, payload.body)
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
	service := startTestService(t, upstream.URL, false, io.Discard)
	request := httptest.NewRequest("POST", "/v1/responses", nil)
	request.Body = &unreadBody{}
	request.ContentLength = -1
	request.Header.Set("Authorization", "Bearer client-key")
	writer := httptest.NewRecorder()
	service.ServeHTTP(writer, request)
	if writer.Code != http.StatusBadRequest || !strings.Contains(writer.Body.String(), `"type":"invalid_request_error"`) || !strings.Contains(writer.Body.String(), `"message":"cannot read request body"`) {
		t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
	}
}

func TestStreamingPathDoesNotReadBodyBeforeCredentials(t *testing.T) {
	service := startTestService(t, "http://unused", false, io.Discard)
	service.credentialStore.path += ".missing"
	request := httptest.NewRequest("POST", "/v1/files", nil)
	body := &unreadBody{}
	request.Body = body
	request.ContentLength = -1
	request.Header.Set("Authorization", "Bearer client-key")
	writer := httptest.NewRecorder()
	service.ServeHTTP(writer, request)
	if writer.Code != http.StatusUnauthorized || body.reads != 0 {
		t.Fatalf("status=%d reads=%d", writer.Code, body.reads)
	}
}

func TestWebSocketUpgradeDoesNotInspectBody(t *testing.T) {
	request := httptest.NewRequest("GET", "/v1/responses", nil)
	request.Header.Set("Upgrade", "websocket")
	body := &unreadBody{}
	request.Body = body
	_, err := (&Service{}).readRequestModel(httptest.NewRecorder(), request)
	if err != nil || body.reads != 0 {
		t.Fatalf("reads=%d err=%v", body.reads, err)
	}
}
