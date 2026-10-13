package ocm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type delayedEOFTransport struct {
	base    http.RoundTripper
	entered chan struct{}
	release chan struct{}
}

func (rt delayedEOFTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := rt.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = &delayedEOFBody{ReadCloser: response.Body, entered: rt.entered, release: rt.release}
	return response, nil
}

type delayedEOFBody struct {
	io.ReadCloser
	entered chan struct{}
	release chan struct{}
}

func (b *delayedEOFBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if err == io.EOF {
		close(b.entered)
		select {
		case <-b.release:
		case <-time.After(2 * time.Second):
			return n, context.DeadlineExceeded
		}
	}
	return n, err
}

func TestCloseWaitsForHTTPUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+completedEvent+"\n\n")
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()
	service := startTestService(t, upstream.URL, false, io.Discard)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	service.httpClient.Transport = delayedEOFTransport{service.httpClient.Transport, entered, release}
	request, err := http.NewRequest("POST", "http://"+service.listener.Addr().String()+"/v1/responses", strings.NewReader(`{"model":"gpt-5.4"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer client-key")
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	awaitSignal(t, entered)
	closed := make(chan error, 1)
	go func() { closed <- service.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned before HTTP handler completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after HTTP handler exited")
	}
	assertSavedUsage(t, testUsagePath(service))
}

func TestCloseCancelsRefreshAndRejectsNewRequests(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(canceled)
	}))
	defer endpoint.Close()
	service := startTestService(t, "http://unused", true, io.Discard)
	service.credentialStore.tokenURL = endpoint.URL
	credentials := &oauthCredentials{Tokens: &tokenData{RefreshToken: "old-refresh"}}
	if err := writeCredentialsToFile(context.Background(), credentials, service.credentialStore.path); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer client-key")
	finished := make(chan struct{})
	go func() { service.ServeHTTP(httptest.NewRecorder(), request); close(finished) }()
	awaitSignal(t, entered)
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, finished)
	awaitSignal(t, canceled)
	writer := httptest.NewRecorder()
	service.ServeHTTP(writer, request)
	if writer.Code != http.StatusServiceUnavailable {
		t.Fatalf("request admitted after Close: %d", writer.Code)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}
