package proxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ocm/internal/config"
)

func TestHTTPTransportTimeouts(t *testing.T) {
	client := NewHTTPClient(&net.Dialer{})
	defer client.CloseIdleConnections()
	transport := client.Transport.(*http.Transport)
	if transport.ResponseHeaderTimeout != 0 || transport.TLSHandshakeTimeout != 10*time.Second || transport.IdleConnTimeout != 90*time.Second || transport.MaxIdleConnsPerHost != 16 {
		t.Fatalf("unexpected transport configuration: %+v", transport)
	}
}

func TestBearerSchemeCase(t *testing.T) {
	manager := &UserManager{}
	manager.UpdateUsers([]config.User{{Name: "test", Token: "CaseSensitive"}})
	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "bEaReR"} {
		for _, statistics := range []bool{false, true} {
			request := httptest.NewRequest("GET", "/", nil)
			request.Header.Set("Authorization", scheme+" CaseSensitive")
			name, failure := manager.authorize(request, statistics)
			if name != "test" || failure != Authenticated {
				t.Fatalf("%s: %s %v", scheme, name, failure)
			}
			request.Header.Set("Authorization", scheme+" casesensitive")
			if _, failure := manager.authorize(request, statistics); failure != UnknownToken {
				t.Fatalf("token case ignored: %v", failure)
			}
		}
	}
}

func TestLoopbackListen(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "127.10.20.30", "127.255.255.255", "::1", "localhost", "LOCALHOST"} {
		if !IsLoopbackListen(host) {
			t.Errorf("%q should be loopback", host)
		}
	}
	for _, host := range []string{"", "0.0.0.0", "::", "192.168.1.1", "128.0.0.1", "localhost.example", "example.com"} {
		if IsLoopbackListen(host) {
			t.Errorf("%q should not be loopback", host)
		}
	}
}

func TestReadRequestBody(t *testing.T) {
	request := httptest.NewRequest("POST", "/", strings.NewReader("body"))
	body, err := ReadRequestBody(httptest.NewRecorder(), request)
	if err != nil || string(body) != "body" {
		t.Fatalf("%q, %v", body, err)
	}
}
