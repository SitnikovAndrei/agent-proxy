package ocm

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const jsonResponse = `{"id":"resp_test","model":"gpt-5.4","service_tier":"default","usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":10}}}`
const chatResponse = `{"model":"gpt-5.4","service_tier":"default","usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":10}}}`

func TestGzipResponseUsage(t *testing.T) {
	for _, tc := range []struct {
		name, path, mediaType, body string
	}{
		{"responses JSON", "/v1/responses", "application/json", jsonResponse},
		{"responses SSE", "/v1/responses", "text/event-stream", "data: " + completedEvent + "\n\n"},
		{"chat JSON", "/v1/chat/completions", "application/json", chatResponse},
		{"chat SSE", "/v1/chat/completions", "text/event-stream", "data: " + chatResponse + "\n\ndata: [DONE]\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var compressed strings.Builder
			gzipWriter := gzip.NewWriter(&compressed)
			_, _ = io.WriteString(gzipWriter, tc.body)
			if err := gzipWriter.Close(); err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.mediaType)
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
				_, _ = io.WriteString(w, compressed.String())
			}))
			defer upstream.Close()
			service := startTestService(t, upstream.URL, false, io.Discard)
			request := httptest.NewRequest("POST", tc.path, strings.NewReader(`{"model":"gpt-5.4"}`))
			request.Header.Set("Authorization", "Bearer client-key")
			request.Header.Set("Accept-Encoding", "gzip")
			writer := httptest.NewRecorder()
			service.ServeHTTP(writer, request)
			if writer.Code != http.StatusOK || writer.Body.String() != tc.body || writer.Header().Get("Content-Encoding") != "" || writer.Header().Get("Content-Length") != "" {
				t.Fatalf("incorrect decompressed response: %d %v %q", writer.Code, writer.Header(), writer.Body.String())
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			assertSavedUsage(t, testUsagePath(service))
		})
	}
}

type disconnectedWriter struct{ *httptest.ResponseRecorder }

func (w disconnectedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestUsageSurvivesClientWriteFailure(t *testing.T) {
	for _, tc := range []struct {
		name, path, mediaType, body string
	}{
		{"responses JSON", "/v1/responses", "application/json", jsonResponse},
		{"responses SSE", "/v1/responses", "text/event-stream", "data: " + completedEvent + "\n\n"},
		{"chat JSON", "/v1/chat/completions", "application/json", chatResponse},
		{"chat SSE", "/v1/chat/completions", "text/event-stream", "data: " + chatResponse + "\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := startTestService(t, "http://unused", false, io.Discard)
			response := &http.Response{Header: http.Header{"Content-Type": []string{tc.mediaType}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			service.handleResponseWithTracking(disconnectedWriter{httptest.NewRecorder()}, response, tc.path, "gpt-5.4", "tester")
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			assertSavedUsage(t, testUsagePath(service))
		})
	}
}

func TestSSEUsageBeforeEOF(t *testing.T) {
	service := startTestService(t, "http://unused", false, io.Discard)
	tracker := streamUsageTracker{record: func(usage responseUsage) { service.recordUsage(usage, "gpt-5.4", "tester", nil) }}
	data := "data: " + completedEvent + "\n\n"
	tracker.consume([]byte(data[:35]))
	tracker.consume([]byte(data[35:]))
	assertSavedUsage(t, testUsagePath(service))
	tracker.consume([]byte(data))
	tracker.finish()
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	assertSavedUsage(t, testUsagePath(service))
}
