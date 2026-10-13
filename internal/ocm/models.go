package ocm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"ocm/internal/proxy"
)

// Used only when a client omits the Codex catalog version query parameter.
const codexCatalogClientVersion = "0.162.1"
const maxModelsResponseBytes = 8 << 20

func (s *Service) serveModels(w http.ResponseWriter, incoming *http.Request, path string, credentials *oauthCredentials) {
	ctx, cancel := context.WithTimeout(incoming.Context(), 30*time.Second)
	defer cancel()
	request, err := s.buildProxyRequest(incoming.WithContext(ctx), path, credentials)
	if err != nil {
		s.writeModelsError(w, incoming, err)
		return
	}
	query := request.URL.Query()
	if !query.Has("client_version") {
		query.Set("client_version", codexCatalogClientVersion)
		request.URL.RawQuery = query.Encode()
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := s.httpClient.Do(request)
	if err != nil {
		s.writeModelsError(w, incoming, err)
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		proxy.CopyHeaders(w.Header(), response.Header)
		w.WriteHeader(response.StatusCode)
		s.copyResponse(w, response)
		return
	}
	s.writeModelsResponse(w, incoming, response)
}

func (s *Service) writeModelsResponse(w http.ResponseWriter, incoming *http.Request, response *http.Response) {
	if err := proxy.DecodeGzipResponse(response); err != nil {
		s.writeModelsError(w, incoming, err)
		return
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		s.writeModelsError(w, incoming, errors.New("unsupported model catalog encoding"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxModelsResponseBytes+1))
	if err != nil {
		s.writeModelsError(w, incoming, err)
		return
	}
	if len(body) > maxModelsResponseBytes {
		s.writeModelsError(w, incoming, errors.New("model catalog exceeds size limit"))
		return
	}
	body, err = compatibleModelsResponse(body)
	if err != nil {
		s.writeModelsError(w, incoming, err)
		return
	}
	proxy.CopyHeaders(w.Header(), response.Header)
	for _, header := range []string{"Content-Length", "Content-Encoding", "Content-MD5", "Digest", "ETag"} {
		w.Header().Del(header)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Service) writeModelsError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("read upstream model catalog", "error", proxy.TransportFailure(err))
	writeJSONError(w, r, http.StatusBadGateway, "api_error", "upstream model catalog request failed")
}
