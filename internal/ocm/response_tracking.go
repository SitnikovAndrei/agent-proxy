package ocm

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"time"

	"ocm/internal/proxy"
	"ocm/internal/statistics"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

type responseUsage struct {
	model        string
	serviceTier  statistics.ServiceTier
	inputTokens  int64
	outputTokens int64
	cachedTokens int64
}

func usageFromResponse(response responses.Response) (responseUsage, error) {
	tier, err := statistics.ParseServiceTier(string(response.ServiceTier))
	if err != nil {
		return responseUsage{}, err
	}
	return responseUsage{
		model: string(response.Model), serviceTier: tier,
		inputTokens: response.Usage.InputTokens, outputTokens: response.Usage.OutputTokens,
		cachedTokens: response.Usage.InputTokensDetails.CachedTokens,
	}, nil
}

func (s *Service) recordUsage(usage responseUsage, requestModel, username string, cycle *WeeklyCycleHint) {
	if usage.inputTokens == 0 && usage.outputTokens == 0 {
		return
	}
	if usage.model == "" {
		usage.model = requestModel
	}
	if usage.model == "" {
		return
	}
	window := detectContextWindow(usage.model, usage.serviceTier, usage.inputTokens)
	if err := s.usageTracker.AddUsageWithCycleHint(usage.model, window, usage.inputTokens,
		usage.outputTokens, usage.cachedTokens, usage.serviceTier, username, time.Now(), cycle); err != nil {
		s.logger.Error("record usage", "error", err)
	}
}

func isStreamingResponse(response *http.Response, chat bool) bool {
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return (err == nil && mediaType == "text/event-stream") || (!chat && response.Header.Get("Content-Type") == "")
}

func (s *Service) handleResponseWithTracking(writer http.ResponseWriter, response *http.Response, path, requestModel, username string) {
	chat := path == "/v1/chat/completions"
	cycle := extractWeeklyCycleHint(response.Header)
	record := func(usage responseUsage) { s.recordUsage(usage, requestModel, username, cycle) }
	if !isStreamingResponse(response, chat) {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			s.logger.Error("read response body", "error", err)
			return
		}
		if usage, err := parseJSONUsage(body, chat); err == nil {
			record(usage)
		} else {
			s.logger.Error("parse response usage", "error", err)
		}
		_, _ = writer.Write(body)
		return
	}
	tracker := streamUsageTracker{chat: chat, record: record, onError: func(err error) {
		s.logger.Error("parse streaming usage", "error", err)
	}}
	defer tracker.finish()
	s.copyStream(writer, response.Body, tracker.consume)
}

func parseJSONUsage(body []byte, chat bool) (responseUsage, error) {
	if chat {
		var completion openai.ChatCompletion
		if err := json.Unmarshal(body, &completion); err != nil {
			return responseUsage{}, err
		}
		tier, err := statistics.ParseServiceTier(string(completion.ServiceTier))
		if err != nil {
			return responseUsage{}, err
		}
		return responseUsage{
			model: completion.Model, serviceTier: tier,
			inputTokens: completion.Usage.PromptTokens, outputTokens: completion.Usage.CompletionTokens,
			cachedTokens: completion.Usage.PromptTokensDetails.CachedTokens,
		}, nil
	}
	var response responses.Response
	if err := json.Unmarshal(body, &response); err != nil {
		return responseUsage{}, err
	}
	return usageFromResponse(response)
}

type streamUsageTracker struct {
	chat       bool
	pending    []byte
	usage      responseUsage
	recorded   bool
	record     func(responseUsage)
	onError    func(error)
	parseError error
}

func (t *streamUsageTracker) consume(data []byte) {
	t.pending = append(t.pending, data...)
	for {
		line, rest, found := bytes.Cut(t.pending, []byte("\n"))
		if !found {
			return
		}
		t.consumeLine(line)
		t.pending = rest
	}
}

func (t *streamUsageTracker) consumeLine(line []byte) {
	data, found := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
	if !found || t.recorded || t.parseError != nil {
		return
	}
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("[DONE]")) {
		return
	}
	if t.chat {
		t.consumeChatChunk(data)
		return
	}
	var event struct {
		Type     string             `json:"type"`
		Response responses.Response `json:"response"`
	}
	if json.Unmarshal(data, &event) == nil && event.Type == "response.completed" {
		usage, err := usageFromResponse(event.Response)
		if err != nil {
			t.fail(err)
			return
		}
		t.usage = usage
		t.save()
	}
}

func (t *streamUsageTracker) consumeChatChunk(data []byte) {
	var chunk openai.ChatCompletionChunk
	if json.Unmarshal(data, &chunk) != nil {
		return
	}
	if chunk.Model != "" {
		t.usage.model = chunk.Model
	}
	if chunk.ServiceTier != "" {
		tier, err := statistics.ParseServiceTier(string(chunk.ServiceTier))
		if err != nil {
			t.fail(err)
			return
		}
		t.usage.serviceTier = tier
	}
	if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
		t.usage.inputTokens = chunk.Usage.PromptTokens
		t.usage.outputTokens = chunk.Usage.CompletionTokens
		t.usage.cachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
		t.save()
	}
}

func (t *streamUsageTracker) save() {
	if !t.recorded && t.parseError == nil {
		t.record(t.usage)
		t.recorded = true
	}
}

func (t *streamUsageTracker) finish() {
	t.consumeLine(t.pending)
	t.save()
}

func (s *Service) copyResponse(writer http.ResponseWriter, response *http.Response) {
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err == nil && mediaType != "text/event-stream" {
		_, _ = io.Copy(writer, response.Body)
		return
	}
	s.copyStream(writer, response.Body, func([]byte) {})
}

func (s *Service) copyStream(writer http.ResponseWriter, body io.Reader, consume func([]byte)) {
	if err := proxy.CopyStream(writer, body, consume); err != nil {
		s.logger.Debug("copy streaming response", "error", err)
	}
}

func (t *streamUsageTracker) fail(err error) {
	t.parseError = err
	if t.onError != nil {
		t.onError(err)
	}
}
