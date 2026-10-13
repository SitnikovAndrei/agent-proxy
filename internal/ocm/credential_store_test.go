package ocm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testCredentialStore(t *testing.T, endpoint string, timeout time.Duration) *credentialStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	credentials := &oauthCredentials{Tokens: &tokenData{AccessToken: "old-access", RefreshToken: "old-refresh", AccountID: "account"}}
	if err := writeCredentialsToFile(context.Background(), credentials, path); err != nil {
		t.Fatal(err)
	}
	return newCredentialStore(path, &http.Client{}, endpoint, timeout)
}

func TestCredentialRefreshDeadline(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer endpoint.Close()
	store := testCredentialStore(t, endpoint.URL, 100*time.Millisecond)
	_, err := store.load(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refresh should time out, got %v", err)
	}
}

func TestCredentialRefreshCancellation(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
		close(canceled)
	}))
	defer endpoint.Close()
	store := testCredentialStore(t, endpoint.URL, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := store.load(ctx); finished <- err }()
	awaitSignal(t, entered)
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("refresh should be canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh did not stop after cancellation")
	}
	awaitSignal(t, canceled)
}

func TestConcurrentCredentialRefresh(t *testing.T) {
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh"}`)
	}))
	defer endpoint.Close()
	store := testCredentialStore(t, endpoint.URL, time.Second)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			credentials, err := store.load(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			if credentials.getAccessToken() != "new-access" || credentials.getAccountID() != "account" {
				t.Error("refreshed token or account ID missing")
			}
		}()
	}
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refresh performed %d times", calls.Load())
	}
}

func TestCredentialChangeDuringRefresh(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, `{"access_token":"refreshed-old-account"}`)
	}))
	defer endpoint.Close()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	store := testCredentialStore(t, endpoint.URL, time.Second)
	finished := make(chan error, 1)
	go func() {
		credentials, err := store.load(context.Background())
		if err == nil && credentials.getAccessToken() != "replacement-key" {
			err = errors.New("external credentials overwritten by stale refresh")
		}
		finished <- err
	}()
	awaitSignal(t, entered)
	if err := writeCredentialsToFile(context.Background(), &oauthCredentials{APIKey: "replacement-key"}, store.path); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh did not finish")
	}
	credentials, err := readCredentialsFromFile(context.Background(), store.path)
	if err != nil || credentials.getAccessToken() != "replacement-key" {
		t.Fatal("replacement credentials not preserved on disk")
	}
}

func TestCredentialRefreshWaiterDeadline(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, `{"access_token":"new-access"}`)
	}))
	defer endpoint.Close()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	store := testCredentialStore(t, endpoint.URL, time.Second)
	leader := make(chan error, 1)
	go func() { _, err := store.load(context.Background()); leader <- err }()
	awaitSignal(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := store.load(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting for refresh should respect its deadline, got %v", err)
	}
	// A newly logged-in account remains usable while the old account's refresh is blocked.
	if err := writeCredentialsToFile(context.Background(), &oauthCredentials{APIKey: "replacement-key"}, store.path); err != nil {
		t.Fatal(err)
	}
	if credentials, err := store.load(context.Background()); err != nil || credentials.APIKey != "replacement-key" {
		t.Fatal("fresh credentials blocked behind refresh")
	}
	unblock()
	select {
	case err := <-leader:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh leader did not finish")
	}
}

func TestCredentialFileRemovalAndRecovery(t *testing.T) {
	store := testCredentialStore(t, "http://unused", time.Second)
	if err := os.Remove(store.path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(context.Background()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted credentials should be unavailable, got %v", err)
	}
	if err := os.WriteFile(store.path, []byte("not JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(context.Background()); err == nil {
		t.Fatal("invalid credentials should be unavailable")
	}
	if err := writeCredentialsToFile(context.Background(), &oauthCredentials{APIKey: "replacement-key"}, store.path); err != nil {
		t.Fatal(err)
	}
	if credentials, err := store.load(context.Background()); err != nil || credentials.APIKey != "replacement-key" {
		t.Fatal("recreated credentials not loaded")
	}
}

type cancelAfterRefreshTransport struct{ cancel context.CancelFunc }

func (rt cancelAfterRefreshTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := &cancelAfterRefreshBody{Reader: strings.NewReader(`{"access_token":"new-access","refresh_token":"new-refresh"}`), cancel: rt.cancel}
	return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header), Request: request}, nil
}

type cancelAfterRefreshBody struct {
	io.Reader
	cancel context.CancelFunc
}

func (b *cancelAfterRefreshBody) Close() error {
	b.cancel()
	return nil
}

func TestSuccessfulRefreshPersistsAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := testCredentialStore(t, "http://unused", time.Second)
	store.client.Transport = cancelAfterRefreshTransport{cancel}
	if _, err := store.load(ctx); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("caller was not canceled after token response")
	}
	credentials, err := readCredentialsFromFile(context.Background(), store.path)
	if err != nil || credentials.getAccessToken() != "new-access" || credentials.Tokens.RefreshToken != "new-refresh" {
		t.Fatal("rotated tokens lost after successful refresh and caller cancellation")
	}
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not reach expected boundary")
	}
}
