package ccm

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"ocm/internal/proxy"
)

func isStreaming(response *http.Response) bool {
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return err == nil && mediaType == "text/event-stream"
}

func (s *Service) copyResponse(writer http.ResponseWriter, response *http.Response, consume func([]byte)) {
	var err error
	if isStreaming(response) {
		err = proxy.CopyStream(writer, response.Body, consume)
	} else {
		_, err = io.Copy(writer, response.Body)
	}
	if err != nil {
		s.logger.Debug("copy Claude response", "error", err)
	}
}

func (s *Service) handleTrackedResponse(writer http.ResponseWriter, response *http.Response, metadata requestMetadata) {
	week := weekStartUnix(response.Header)
	if !isStreaming(response) {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			s.logger.Error("read Claude response", "error", err)
			_, _ = writer.Write(body)
			return
		}
		var message responseMessage
		if err := json.Unmarshal(body, &message); err != nil {
			s.logger.Error("parse Claude usage", "error", err)
		} else {
			s.recordUsage(message, metadata, week)
		}
		_, _ = writer.Write(body)
		return
	}
	tracker := streamUsageTracker{logger: s.logger, record: func(message responseMessage) { s.recordUsage(message, metadata, week) }}
	defer tracker.finish()
	s.copyResponse(writer, response, tracker.consume)
}

type streamUsageTracker struct {
	pending  []byte
	splitter *proxy.SSELineSplitter
	message  responseMessage
	record   func(responseMessage)
	logger   *slog.Logger
	recorded bool
	started  bool
	disabled bool
}

func (t *streamUsageTracker) consume(data []byte) {
	if t.splitter == nil {
		t.splitter = &proxy.SSELineSplitter{Buffer: &t.pending,
			Max: 16 << 20, OnLine: t.consumeLine, PendingOnly: true,
			Done:       func() bool { return t.recorded || t.disabled },
			OnOverflow: func() { t.logger.Error("Claude usage event exceeds 16 MiB"); t.disabled = true },
		}
	}
	t.splitter.Write(data)
}

func (t *streamUsageTracker) consumeLine(line []byte) {
	data, found := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
	if !found || t.recorded || t.disabled {
		return
	}
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("[DONE]")) {
		return
	}
	var event streamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.logger.Error("parse Claude stream event", "error", err)
		return
	}
	if event.Type == nil {
		t.logger.Error("Claude stream event has no type")
		return
	}
	switch *event.Type {
	case messageStart:
		t.message = event.Message
		t.started = true
	case messageDelta:
		t.message.Usage.apply(event.Usage)
	case messageStop, streamError:
		t.save()
	case unknownStreamEvent, contentBlockStart, contentBlockDelta, contentBlockStop, streamPing:
	}
}

func (t *streamUsageTracker) save() {
	if t.started && !t.recorded {
		t.record(t.message)
		t.recorded = true
	}
}

func (t *streamUsageTracker) finish() {
	if t.splitter != nil {
		t.splitter.Flush()
	}
	t.save()
}
