package ocm

import (
	"bytes"
	"encoding/json"
	"errors"
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
	pending    []byte
	chat       bool
	splitter   *proxy.SSELineSplitter
	usage      responseUsage
	recorded   bool
	record     func(responseUsage)
	onError    func(error)
	parseError error
}

const maxUsageEventBytes = 16 << 20

func isFinalResponseEvent(eventType string) bool {
	switch eventType {
	case "response.completed", "response.incomplete", "response.failed":
		return true
	default:
		return false
	}
}

func (t *streamUsageTracker) consume(data []byte) {
	if t.splitter == nil {
		t.splitter = &proxy.SSELineSplitter{Buffer: &t.pending,
			Max: maxUsageEventBytes, OnLine: t.consumeLine,
			Done:       func() bool { return t.recorded || t.parseError != nil },
			OnOverflow: func() { t.fail(errors.New("OpenAI usage event exceeds 16 MiB")) },
		}
	}
	t.splitter.Write(data)
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
	var kind struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &kind) != nil || !isFinalResponseEvent(kind.Type) {
		return
	}
	var event struct {
		Type     string             `json:"type"`
		Response responses.Response `json:"response"`
	}
	if json.Unmarshal(data, &event) == nil {
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
	var chunk struct {
		Model       string `json:"model"`
		ServiceTier string `json:"service_tier"`
		Usage       struct {
			PromptTokens        int64 `json:"prompt_tokens"`
			CompletionTokens    int64 `json:"completion_tokens"`
			PromptTokensDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
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
	if t.splitter != nil {
		t.splitter.Flush()
	}
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
