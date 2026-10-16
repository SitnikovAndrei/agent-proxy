package ocm

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSSEFinalUsage(t *testing.T) {
	for _, kind := range []string{"response.completed", "response.incomplete", "response.failed"} {
		t.Run(kind, func(t *testing.T) {
			saved := 0
			tracker := streamUsageTracker{record: func(usage responseUsage) {
				saved++
				if usage.inputTokens != 100 || usage.outputTokens != 20 {
					t.Fatalf("usage = %+v", usage)
				}
			}}
			data := []byte("data: " + strings.Replace(completedEvent, "response.completed", kind, 1) + "\n\n")
			for _, b := range data {
				tracker.consume([]byte{b})
			}
			tracker.consume(data)
			tracker.consume(bytes.Repeat([]byte("x"), maxUsageEventBytes+1))
			tracker.finish()
			if saved != 1 || len(tracker.pending) != 0 || tracker.parseError != nil {
				t.Fatalf("saved=%d pending=%d error=%v", saved, len(tracker.pending), tracker.parseError)
			}
		})
	}
}

func TestSSEPendingLimitAndDisable(t *testing.T) {
	errors := 0
	saved := 0
	tracker := streamUsageTracker{record: func(responseUsage) { saved++ }, onError: func(error) { errors++ }}
	tracker.consume(bytes.Repeat([]byte("x"), maxUsageEventBytes))
	if len(tracker.pending) != maxUsageEventBytes || tracker.parseError != nil {
		t.Fatal("exact limit rejected")
	}
	tracker.consume([]byte("x"))
	tracker.consume([]byte("data: " + completedEvent + "\n"))
	tracker.finish()
	if errors != 1 || saved != 0 || tracker.pending != nil {
		t.Fatalf("errors=%d saved=%d pending=%d", errors, saved, len(tracker.pending))
	}
}

func TestSSEParsingFailureStopsBuffering(t *testing.T) {
	tracker := streamUsageTracker{record: func(responseUsage) { t.Fatal("must not record") }}
	tracker.consume([]byte("data: " + strings.Replace(completedEvent, `"default"`, `"future-tier"`, 1) + "\n"))
	tracker.consume(bytes.Repeat([]byte("x"), maxUsageEventBytes+1))
	if tracker.parseError == nil || len(tracker.pending) != 0 {
		t.Fatal("failed tracker kept buffering")
	}
}

func TestChatFailedRecordsUsageBeforeError(t *testing.T) {
	for _, stream := range []bool{false, true} {
		service := startTestService(t, "http://unused", false, io.Discard)
		event := strings.Replace(completedEvent, "response.completed", "response.failed", 1)
		response := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(chatSSE(event, event)))}
		writer := httptest.NewRecorder()
		service.convertChatResponse(writer, httptest.NewRequest("POST", "/v1/chat/completions", nil), response, chatAdapterRequest{model: "gpt-5.4", stream: stream}, "tester")
		if !strings.Contains(writer.Body.String(), "upstream response failed") {
			t.Fatalf("missing error: %s", writer.Body.String())
		}
		assertSavedUsage(t, testUsagePath(service))
	}
}
