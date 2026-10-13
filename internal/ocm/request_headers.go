package ocm

import (
	"net/http"
	"strings"

	"ocm/internal/proxy"
)

const codexOriginator = "Codex Desktop"
const codexUserAgent = "Codex Desktop/0.162.0-alpha.17.2 (Windows 10.0.26200; x86_64) unknown (codex_chatgpt_android_remote; dev)"

func (s *Service) upstreamHeaders(incoming http.Header, credentials *oauthCredentials) http.Header {
	combined := make(http.Header)
	proxy.CopyHeaders(combined, incoming)
	for key := range combined {
		if !isAllowedClientHeader(key) {
			combined.Del(key)
		}
	}
	for key, values := range s.httpHeaders {
		combined[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	headers := make(http.Header)
	proxy.CopyHeaders(headers, combined)
	headers.Del("Authorization")
	headers.Del("X-Api-Key")
	headers.Del("Cookie")
	headers.Del("ChatGPT-Account-Id")
	headers.Del("X-Openai-Internal-Codex-Residency")
	headers.Set("User-Agent", codexUserAgent)
	headers.Set("Originator", codexOriginator)
	headers.Set("Authorization", "Bearer "+credentials.getAccessToken())
	if accountID := credentials.getAccountID(); accountID != "" {
		headers.Set("ChatGPT-Account-Id", accountID)
	}
	if residency := credentials.getResidency(); residency != nil {
		headers.Set("X-Openai-Internal-Codex-Residency", *residency)
	}
	return headers
}

func isAllowedClientHeader(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Oai-Product-Sku", "Accept":
		return true
	case "Content-Type", "Content-Encoding", "Accept-Encoding", "Openai-Beta":
		return true
	default:
		return false
	}
}

func (s *Service) upstreamWebSocketHeaders(incoming http.Header, credentials *oauthCredentials) http.Header {
	headers := s.upstreamHeaders(incoming, credentials)
	for key := range headers {
		if strings.HasPrefix(strings.ToLower(key), "sec-websocket-") {
			headers.Del(key)
		}
	}
	return headers
}
