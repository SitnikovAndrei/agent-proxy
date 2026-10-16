package proxy

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ocm/internal/config"
)

func lifecycleFixture(t *testing.T) *Lifecycle {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	lifecycle := NewLifecycle(ctx, cancel, slog.New(slog.NewTextHandler(io.Discard, nil)), &http.Client{}, context.DeadlineExceeded)
	t.Cleanup(func() {
		if err := lifecycle.Close(); err != nil {
			t.Error(err)
		}
	})
	return lifecycle
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestLifecycleWaitsAndCancels(t *testing.T) {
	lifecycle := lifecycleFixture(t)
	entered, canceled, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	go func() {
		defer close(finished)
		lifecycle.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()
			close(canceled)
			<-release
		}, func(http.ResponseWriter, *http.Request) { t.Error("unexpected rejection") })
	}()
	waitSignal(t, entered)
	closed := make(chan error, 1)
	go func() { closed <- lifecycle.Close() }()
	waitSignal(t, canceled)
	select {
	case err := <-closed:
		t.Fatalf("Close returned before handler: %v", err)
	default:
	}
	rejected := false
	lifecycle.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), func(http.ResponseWriter, *http.Request) { t.Error("request accepted after stopping") }, func(http.ResponseWriter, *http.Request) { rejected = true })
	if !rejected || !lifecycle.IsStopping() {
		t.Fatal("shutdown not visible")
	}
	unblock()
	waitSignal(t, finished)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish")
	}
	if err := lifecycle.Close(); err != nil {
		t.Fatal(err)
	}
}

type trackedSession struct{ closed atomic.Int32 }

func (s *trackedSession) Close() { s.closed.Add(1) }

func TestLifecycleTracksSessions(t *testing.T) {
	lifecycle := lifecycleFixture(t)
	active, removed := &trackedSession{}, &trackedSession{}
	if !lifecycle.Register(active) || !lifecycle.Register(removed) {
		t.Fatal("session rejected")
	}
	lifecycle.Unregister(removed)
	if err := lifecycle.Close(); err != nil {
		t.Fatal(err)
	}
	if active.closed.Load() != 1 || removed.closed.Load() != 0 {
		t.Fatal("incorrect sessions closed")
	}
	if lifecycle.Register(&trackedSession{}) {
		t.Fatal("session admitted after shutdown")
	}
}

func TestLifecycleRegistrationDuringShutdown(t *testing.T) {
	for range 30 {
		lifecycle := lifecycleFixture(t)
		session := &trackedSession{}
		admitted := make(chan bool, 1)
		go func() { admitted <- lifecycle.Register(session) }()
		if err := lifecycle.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case accepted := <-admitted:
			if accepted && session.closed.Load() != 1 {
				t.Fatal("registered session escaped shutdown")
			}
		case <-time.After(time.Second):
			t.Fatal("registration blocked")
		}
	}
}

func TestLifecycleStart(t *testing.T) {
	lifecycle := lifecycleFixture(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	logged := false
	listener, err := lifecycle.Start(config.Config{Listen: "127.0.0.1", ListenPort: 0}, handler, func(net.Addr) { logged = true })
	if err != nil {
		t.Fatal(err)
	}
	if !logged || lifecycle.server.ReadHeaderTimeout != 15*time.Second || lifecycle.server.IdleTimeout != 90*time.Second {
		t.Fatal("server configuration changed")
	}
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal(response.StatusCode)
	}
}
