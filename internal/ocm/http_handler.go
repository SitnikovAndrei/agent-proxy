package ocm

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"ocm/internal/proxy"
)

type requestPayload struct {
	model string
	body  []byte
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.ServeHTTP(w, r, s.serveRequest, func(w http.ResponseWriter, r *http.Request) {
		writeJSONError(w, r, http.StatusServiceUnavailable, "api_error", "service is shutting down")
	})
}

func (s *Service) serveRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/stats" {
		s.statistics.ServeHTTP(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		writeJSONError(w, r, http.StatusNotFound, "invalid_request_error", "path must start with /v1/")
		return
	}
	username, authenticated := s.authenticate(w, r)
	if !authenticated {
		return
	}
	payload, err := s.readRequestModel(w, r)
	if err != nil {
		status, message := proxy.RequestBodyError(err)
		writeJSONError(w, r, status, "invalid_request_error", message)
		return
	}
	credentials, err := s.credentialStore.read(r.Context())
	if err != nil {
		s.writeCredentialError(w, r, err)
		return
	}
	path := r.URL.Path
	if credentials.needsRefresh() {
		credentials, err = s.credentialStore.load(r.Context())
		if err != nil {
			s.writeCredentialError(w, r, err)
			return
		}
	}
	if path == "/v1/chat/completions" && !credentials.isAPIKeyMode() {
		s.serveChatCompletions(w, r, username, credentials, payload.body)
		return
	}
	proxyPath := path
	if !credentials.isAPIKeyMode() {
		proxyPath = strings.TrimPrefix(path, "/v1")
	}
	if r.Method == http.MethodGet && path == "/v1/models" && !credentials.isAPIKeyMode() {
		s.serveModels(w, r, proxyPath, credentials, payload.body)
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && strings.HasPrefix(path, "/v1/responses") {
		s.handleWebSocket(w, r, proxyPath, username, credentials)
		return
	}
	s.proxyHTTP(w, r, proxyPath, username, credentials, payload)
}

func (s *Service) writeCredentialError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("get credentials", "error", err)
	writeJSONError(w, r, http.StatusUnauthorized, "authentication_error", "Authentication failed")
}

func (s *Service) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	username, failure := s.userManager.Authorize(r)
	if failure != proxy.Authenticated {
		s.logger.Warn("authentication failed", "remote", r.RemoteAddr, "reason", failure.Message())
		writeJSONError(w, r, http.StatusUnauthorized, "authentication_error", failure.Message())
		return "", false
	}
	return username, true
}

func (s *Service) proxyHTTP(w http.ResponseWriter, r *http.Request, path, username string, credentials *oauthCredentials, payload requestPayload) {
	request, err := s.buildProxyRequest(r, path, credentials, payload.body)
	if err != nil {
		s.logger.Error("create proxy request", "error", err)
		writeJSONError(w, r, http.StatusInternalServerError, "api_error", "Internal server error")
		return
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		s.logger.Error("request upstream", "error", proxy.TransportFailure(err))
		writeJSONError(w, r, http.StatusBadGateway, "api_error", "upstream request failed")
		return
	}
	defer func() { _ = response.Body.Close() }()
	trackUsage := s.usageTracker != nil && response.StatusCode == http.StatusOK &&
		(r.URL.Path == "/v1/chat/completions" || strings.HasPrefix(r.URL.Path, "/v1/responses"))
	if trackUsage {
		if err := proxy.DecodeGzipResponse(response); err != nil {
			s.logger.Error("decode upstream response", "error", err)
			writeJSONError(w, r, http.StatusBadGateway, "api_error", "invalid compressed upstream response")
			return
		}
	}
	proxy.CopyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	if trackUsage {
		s.handleResponseWithTracking(w, response, r.URL.Path, payload.model, username)
		return
	}
	s.copyResponse(w, response)
}

func inspectsRequestBody(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	return r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/v1/responses" || strings.HasPrefix(r.URL.Path, "/v1/responses/")
}

func (s *Service) readRequestModel(w http.ResponseWriter, r *http.Request) (requestPayload, error) {
	if !inspectsRequestBody(r) {
		return requestPayload{}, nil
	}
	body, err := proxy.ReadRequestBody(w, r)
	if err != nil {
		return requestPayload{}, err
	}
	var request struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return requestPayload{body: body}, nil
	}
	return requestPayload{model: request.Model, body: body}, nil
}

func (s *Service) buildProxyRequest(r *http.Request, path string, credentials *oauthCredentials, body []byte) (*http.Request, error) {
	url := s.baseURL(credentials) + path
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}
	var source io.Reader = r.Body
	inspected := inspectsRequestBody(r)
	if inspected {
		source = bytes.NewReader(body)
	} else if r.ContentLength == 0 || r.Body == nil || r.Body == http.NoBody {
		source = http.NoBody
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, url, source)
	if err != nil {
		return nil, err
	}
	if !inspected {
		request.ContentLength = r.ContentLength
	}
	request.Header = s.upstreamHeaders(r.Header, credentials)
	request.Header.Del("Content-Length")
	return request, nil
}
