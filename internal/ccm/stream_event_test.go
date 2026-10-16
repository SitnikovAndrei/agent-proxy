package ccm

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestUnknownStreamEventIgnored(t *testing.T) {
	var event streamEvent
	if err := json.Unmarshal([]byte(`{"type":"future_event"}`), &event); err != nil || event.Type == nil || *event.Type != unknownStreamEvent {
		t.Fatalf("unknown type: %+v, %v", event, err)
	}
	var logs bytes.Buffer
	saved := 0
	tracker := streamUsageTracker{logger: slog.New(slog.NewTextHandler(&logs, nil)), record: func(responseMessage) { saved++ }}
	tracker.consume([]byte("data: {\"type\":\"future_event\"}\n\n"))
	if logs.Len() != 0 || saved != 0 {
		t.Fatalf("unknown event should be silent: %s saved=%d", logs.String(), saved)
	}
	tracker.consume([]byte("data: {invalid}\n\n"))
	if logs.Len() == 0 {
		t.Fatal("invalid JSON must log an error")
	}
}
