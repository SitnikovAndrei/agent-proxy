package ccm

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"ocm/internal/proxy"
)

type requestMetadata struct {
	model         string
	messagesCount int
	beta          string
	username      string
}

func writeJSONError(writer http.ResponseWriter, request *http.Request, status int, errorType, message string) {
	response := struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id,omitempty"`
	}{Type: "error", RequestID: request.Header.Get("Request-Id")}
	response.Error.Type = errorType
	response.Error.Message = message
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(response)
}

func (s *Service) serveRequest(writer http.ResponseWriter, incoming *http.Request) {
	if incoming.URL.Path == "/stats" {
		s.statistics.ServeHTTP(writer, incoming)
		return
	}
	if !strings.HasPrefix(incoming.URL.Path, "/v1/") {
		writeJSONError(writer, incoming, http.StatusNotFound, "not_found_error", "Not found")
		return
	}
	username, failure := s.users.Authorize(incoming)
	if failure != proxy.Authenticated {
		s.logger.Warn("authentication failed", "remote", incoming.RemoteAddr, "reason", failure.Message())
		writeJSONError(writer, incoming, http.StatusUnauthorized, "authentication_error", failure.Message())
		return
	}
	metadata, body, err := s.readRequestMetadata(writer, incoming)
	if err != nil {
		status, message := proxy.RequestBodyError(err)
		errorType := "invalid_request_error"
		if status == http.StatusRequestEntityTooLarge {
			errorType = "request_too_large"
		}
		writeJSONError(writer, incoming, status, errorType, message)
		return
	}
	metadata.username = username
	credentials, err := s.credentials.load(incoming.Context())
	if err != nil {
		s.logger.Error("get Claude credentials", "error", err)
		writeJSONError(writer, incoming, http.StatusUnauthorized, "authentication_error", "Authentication failed")
		return
	}
	request, err := s.buildProxyRequest(incoming, credentials, body)
	if err != nil {
		writeJSONError(writer, incoming, http.StatusInternalServerError, "api_error", "cannot create upstream request")
		return
	}
	metadata.beta = request.Header.Get("Anthropic-Beta")
	s.proxyRequest(writer, incoming, request, metadata)
}

func inspectsRequestBody(request *http.Request) bool {
	return request.URL.Path == "/v1/messages" || request.URL.Path == "/v1/messages/count_tokens"
}

func (s *Service) readRequestMetadata(writer http.ResponseWriter, request *http.Request) (requestMetadata, []byte, error) {
	if !inspectsRequestBody(request) {
		return requestMetadata{}, nil, nil
	}
	body, err := proxy.ReadRequestBody(writer, request)
	if err != nil {
		return requestMetadata{}, nil, err
	}
	var payload struct {
		Model    string            `json:"model"`
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return requestMetadata{}, body, nil
	}
	return requestMetadata{model: payload.Model, messagesCount: len(payload.Messages)}, body, nil
}

func (s *Service) buildProxyRequest(incoming *http.Request, credentials oauthCredentials, body []byte) (*http.Request, error) {
	var source io.Reader = incoming.Body
	inspected := inspectsRequestBody(incoming)
	if inspected {
		source = bytes.NewReader(body)
	} else if incoming.ContentLength == 0 || incoming.Body == nil || incoming.Body == http.NoBody {
		source = http.NoBody
	}
	request, err := http.NewRequestWithContext(incoming.Context(), incoming.Method, s.baseURL+incoming.URL.RequestURI(), source)
	if err != nil {
		return nil, err
	}
	if !inspected {
		request.ContentLength = incoming.ContentLength
	}
	proxy.CopyHeaders(request.Header, incoming.Header)
	request.Header.Del("Authorization")
	request.Header.Del("X-Api-Key")
	if s.usage != nil {
		request.Header.Del("Accept-Encoding")
	}
	for key, values := range s.headers {
		request.Header[key] = append([]string(nil), values...)
	}
	// OAuth capability is required even when users configure additional beta flags.
	beta := request.Header.Get("Anthropic-Beta")
	flags := strings.Split(beta, ",")
	found := false
	for _, flag := range flags {
		if strings.TrimSpace(flag) == anthropicBetaOAuthValue {
			found = true
		}
	}
	if !found {
		if beta == "" {
			beta = anthropicBetaOAuthValue
		} else {
			beta = anthropicBetaOAuthValue + "," + beta
		}
		request.Header.Set("Anthropic-Beta", beta)
	}
	request.Header.Del("X-Api-Key")
	request.Header.Del("Content-Length")
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	return request, nil
}

func (s *Service) proxyRequest(writer http.ResponseWriter, incoming, request *http.Request, metadata requestMetadata) {
	response, err := s.client.Do(request)
	if err != nil {
		s.logger.Error("request Claude API", "error", proxy.TransportFailure(err))
		writeJSONError(writer, incoming, http.StatusBadGateway, "api_error", "upstream request failed")
		return
	}
	defer func() { _ = response.Body.Close() }()
	track := s.usage != nil && response.StatusCode == http.StatusOK && incoming.URL.Path == "/v1/messages"
	if track {
		if err := proxy.DecodeGzipResponse(response); err != nil {
			s.logger.Error("decode Claude response", "error", err)
			writeJSONError(writer, incoming, http.StatusBadGateway, "api_error", "invalid compressed upstream response")
			return
		}
	}
	proxy.CopyHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	if track {
		s.handleTrackedResponse(writer, response, metadata)
		return
	}
	s.copyResponse(writer, response, func([]byte) {})
}
