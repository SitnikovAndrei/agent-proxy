package ccm

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"ocm/internal/statistics"
)

const usageStart = `{"type":"message_start","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"cache_creation":{"ephemeral_5m_input_tokens":4}}}}`
const usageDeltaEvent = `{"type":"message_delta","usage":{"output_tokens":7}}`
const usageStop = `{"type":"message_stop"}`

func TestNonStreamingResponseReadFailureForwardsPartialBody(t *testing.T) {
	const body = `{"model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":7},"content":[`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+100))
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	store, err := statistics.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := requestTestService(t, upstream.URL)
	service.usage = &usageTracker{store: store}
	writer := httptest.NewRecorder()
	service.ServeHTTP(writer, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[]}`)))
	if writer.Code != http.StatusOK || writer.Body.String() != body {
		t.Fatalf("partial response changed: %d %q", writer.Code, writer.Body.String())
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_records`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("partial response recorded %d usage records", count)
	}
}

func TestStreamUsageAcrossReads(t *testing.T) {
	saved := 0
	tracker := streamUsageTracker{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), record: func(message responseMessage) {
		saved++
		if message.Model != "claude-sonnet-4-5" || message.Usage.InputTokens != 10 || message.Usage.OutputTokens != 7 || message.Usage.CacheReadInputTokens != 3 || message.Usage.CacheCreation.Ephemeral5mInputTokens != 4 {
			t.Fatalf("usage=%+v", message)
		}
	}}
	data := "data: " + usageStart + "\n\ndata: " + usageDeltaEvent + "\n\ndata: " + usageStop
	for _, b := range []byte(data) {
		tracker.consume([]byte{b})
	}
	tracker.finish()
	tracker.consume([]byte("data: " + usageStop + "\n\n"))
	tracker.finish()
	if saved != 1 {
		t.Fatalf("saved=%d", saved)
	}
}

type byteReader struct{ io.Reader }

func (r byteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

type failingUsageWriter struct{ *httptest.ResponseRecorder }

func (failingUsageWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestTrackedResponsePersistsUsage(t *testing.T) {
	for _, mode := range []string{"stream", "json", "write failure"} {
		t.Run(mode, func(t *testing.T) {
			store, err := statistics.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			service := &Service{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), usage: &usageTracker{store: store}}
			body := "data: " + usageStart + "\n\ndata: " + usageDeltaEvent + "\n\ndata: " + usageStop + "\n\ndata: " + usageStop + "\n\n"
			contentType := "text/event-stream"
			var source io.Reader = byteReader{strings.NewReader(body)}
			if mode == "json" || mode == "write failure" {
				body = `{"model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":7,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"cache_creation":{"ephemeral_5m_input_tokens":4}}}`
				contentType = "application/json"
				source = strings.NewReader(body)
			}
			response := &http.Response{Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(source)}
			defer response.Body.Close()
			recorder := httptest.NewRecorder()
			var writer http.ResponseWriter = recorder
			if mode == "write failure" {
				writer = failingUsageWriter{recorder}
			}
			service.handleTrackedResponse(writer, response, requestMetadata{model: "requested-model", username: "alice", messagesCount: 2})
			if mode != "write failure" && recorder.Body.String() != body {
				t.Fatal("response changed")
			}
			summary, err := store.Summary(context.Background(), statistics.Filter{Provider: statistics.Anthropic, Username: "alice", Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			totals := summary.Totals
			if totals.Requests != 1 || totals.Messages != 2 || totals.InputTokens != 10 || totals.OutputTokens != 7 || totals.CacheReadTokens != 3 || totals.CacheCreationTokens != 4 || totals.CacheCreation5mTokens != 4 || len(summary.ByModel) != 1 || summary.ByModel[0].Model != "claude-sonnet-4-5" {
				t.Fatalf("summary=%+v", summary)
			}
		})
	}
}
