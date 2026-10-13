package proxy

import (
	"net"
	"net/http"
	"time"
)

func NewHTTPClient(dialer *net.Dialer) *http.Client {
	return &http.Client{
		Transport: &http.Transport{ForceAttemptHTTP2: true, Proxy: nil, DialContext: dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 60 * time.Second},
		// The configured backends are fixed; redirects must not forward credentials.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
