package ocm

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"ocm/internal/proxy"
)

func (s *Service) serveChatCompletions(w http.ResponseWriter, r *http.Request, username string, credentials *oauthCredentials, body []byte) {
	if r.Method != http.MethodPost {
		writeJSONError(w, r, http.StatusMethodNotAllowed, "invalid_request_error", "chat completions requires POST")
		return
	}
	if encoding := strings.TrimSpace(r.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		writeJSONError(w, r, http.StatusBadRequest, "invalid_request_error", "unsupported Content-Encoding")
		return
	}
	converted, err := convertChatRequest(body)
	if err != nil {
		writeJSONError(w, r, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	url := s.chatGPTBackendURL + "/responses"
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(converted.body))
	if err != nil {
		writeJSONError(w, r, http.StatusInternalServerError, "api_error", "cannot create upstream request")
		return
	}
	request.Header = s.upstreamHeaders(r.Header, credentials)
	request.Header.Del("Content-Length")
	request.Header.Del("Content-Encoding")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	response, err := s.httpClient.Do(request)
	if err != nil {
		s.logger.Error("request upstream", "error", proxy.TransportFailure(err))
		writeJSONError(w, r, http.StatusBadGateway, "api_error", "upstream request failed")
		return
	}
	defer func() { _ = response.Body.Close() }()
	if err := proxy.DecodeGzipResponse(response); err != nil {
		writeJSONError(w, r, http.StatusBadGateway, "api_error", "invalid compressed upstream response")
		return
	}
	if response.StatusCode != http.StatusOK {
		proxy.CopyHeaders(w.Header(), response.Header)
		w.WriteHeader(response.StatusCode)
		s.copyResponse(w, response)
		return
	}
	s.convertChatResponse(w, r, response, converted, username)
}

func chatResponseHeaders(destination, source http.Header, stream bool) {
	proxy.CopyHeaders(destination, source)
	destination.Del("Content-Type")
	destination.Del("Content-Length")
	destination.Del("Content-Encoding")
	destination.Set("Content-Type", "application/json")
	if stream {
		destination.Set("Content-Type", "text/event-stream")
		destination.Set("Cache-Control", "no-cache")
	}
}

// A failed client write disables output while upstream consumption continues for usage.
type chatStreamWriter struct {
	writer http.ResponseWriter
	failed bool
}

func (w *chatStreamWriter) send(value any) {
	if w.failed {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		w.failed = true
		return
	}
	w.write("data: " + string(data) + "\n\n")
}

func (w *chatStreamWriter) write(data string) {
	if w.failed {
		return
	}
	if _, err := io.WriteString(w.writer, data); err != nil {
		w.failed = true
		return
	}
	if err := http.NewResponseController(w.writer).Flush(); err != nil {
		w.failed = true
	}
}

func (s *Service) convertChatResponse(w http.ResponseWriter, r *http.Request, response *http.Response, request chatAdapterRequest, username string) {
	state := chatAdapterResponse{model: request.model, stream: request.stream, toolIndices: make(map[int]int)}
	output := chatStreamWriter{writer: w}
	chatResponseHeaders(w.Header(), response.Header, request.stream)
	if request.stream {
		w.WriteHeader(http.StatusOK)
	}
	cycle := extractWeeklyCycleHint(response.Header)
	err := readChatEvents(response.Body, func(event adapterEvent) error {
		if output.failed && event.Type != "response.completed" && event.Type != "response.incomplete" && event.Type != "response.failed" {
			return nil
		}
		if event.Type == "response.failed" {
			usage, err := usageFromResponse(event.Response)
			if err != nil {
				s.logger.Error("parse chat adapter usage", "error", err)
			} else {
				s.recordUsage(usage, request.model, username, cycle)
			}
		}
		delta, err := state.consume(event)
		if err != nil {
			return err
		}
		if state.final != nil {
			usage, err := usageFromResponse(*state.final)
			if err != nil {
				s.logger.Error("parse chat adapter usage", "error", err)
			} else {
				s.recordUsage(usage, request.model, username, cycle)
			}
			if request.stream {
				output.send(state.chunk(chatDelta{}, &state.finishReason))
				if request.includeUsage {
					chunk := state.chunk(chatDelta{}, nil)
					chunk.Choices = []chatChunkChoice{}
					usage := state.usage()
					chunk.Usage = &usage
					output.send(chunk)
				}
				output.write("data: [DONE]\n\n")
			}
			return io.EOF // The first final event completes the response and its accounting.
		}
		if delta != nil && request.stream {
			output.send(state.chunk(*delta, nil))
		}
		return nil
	})
	if err == nil && state.final == nil {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		message := err.Error()
		if err == io.ErrUnexpectedEOF {
			message = "upstream stream ended unexpectedly"
		}
		if request.stream {
			output.send(errorResponse{Error: errorDetails{Type: "api_error", Message: message}})
		} else {
			writeJSONError(w, r, http.StatusBadGateway, "api_error", message)
		}
		return
	}
	if !request.stream {
		_ = json.NewEncoder(w).Encode(state.completion())
	}
}
