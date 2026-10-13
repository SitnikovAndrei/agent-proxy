package proxy

import (
	"net/http"
	"strings"
)

func IsHopByHopHeader(header string) bool {
	switch strings.ToLower(header) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "trailer", "transfer-encoding", "upgrade", "host":
		return true
	default:
		return false
	}
}

func CopyHeaders(destination, source http.Header) {
	connectionHeaders := make(map[string]bool)
	for _, line := range source.Values("Connection") {
		for _, name := range strings.Split(line, ",") {
			connectionHeaders[http.CanonicalHeaderKey(strings.TrimSpace(name))] = true
		}
	}
	for key, values := range source {
		if !IsHopByHopHeader(key) && !connectionHeaders[http.CanonicalHeaderKey(key)] {
			destination[key] = append([]string(nil), values...)
		}
	}
}
