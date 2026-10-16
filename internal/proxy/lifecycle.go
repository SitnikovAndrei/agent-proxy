package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"ocm/internal/config"
)

// TrackedCloser is a hijacked connection that HTTP server shutdown cannot close.
type TrackedCloser interface{ Close() }

// Lifecycle serializes handler/session admission with shutdown.
type Lifecycle struct {
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *slog.Logger
	client       *http.Client
	timeoutError error
	mutex        sync.Mutex
	stopping     bool
	requests     sync.WaitGroup
	sessions     map[TrackedCloser]struct{}
	listener     net.Listener
	server       *http.Server
	closeOnce    sync.Once
	closeError   error
}

func NewLifecycle(ctx context.Context, cancel context.CancelFunc, logger *slog.Logger, client *http.Client, timeoutError error) *Lifecycle {
	return &Lifecycle{ctx: ctx, cancel: cancel, logger: logger, client: client, timeoutError: timeoutError, sessions: make(map[TrackedCloser]struct{})}
}

func (s *Lifecycle) Start(options config.Config, handler http.Handler, logListening func(net.Addr)) (net.Listener, error) {
	listener, err := Listen(options)
	if err != nil {
		return nil, err
	}
	s.listener = listener
	if len(options.Users) == 0 && !IsLoopbackListen(options.Listen) {
		s.logger.Warn("API is accessible without authentication on a non-loopback address", "address", listener.Addr().String())
	}
	s.server = &http.Server{Handler: handler, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("serve error", "error", err)
		}
	}()
	logListening(listener.Addr())
	return listener, nil
}

func (s *Lifecycle) ServeHTTP(w http.ResponseWriter, r *http.Request, handle, reject http.HandlerFunc) {
	s.mutex.Lock()
	if s.stopping {
		s.mutex.Unlock()
		reject(w, r)
		return
	}
	s.requests.Add(1)
	s.mutex.Unlock()
	defer s.requests.Done()
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	handle(w, r.WithContext(ctx))
}

func (s *Lifecycle) Register(session TrackedCloser) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.stopping {
		return false
	}
	s.sessions[session] = struct{}{}
	return true
}

func (s *Lifecycle) Unregister(session TrackedCloser) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.sessions, session)
}

func (s *Lifecycle) IsStopping() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.stopping
}

func (s *Lifecycle) Close() error {
	s.closeOnce.Do(func() { s.closeError = s.close() })
	return s.closeError
}

func (s *Lifecycle) close() error {
	defer s.client.CloseIdleConnections()
	s.mutex.Lock()
	s.stopping = true
	sessions := make([]TrackedCloser, 0, len(s.sessions))
	for session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mutex.Unlock()
	s.cancel()
	for _, session := range sessions {
		session.Close()
	}
	var closeError error
	if s.server != nil {
		closeError = s.server.Close()
	}
	if s.listener != nil {
		if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeError = errors.Join(closeError, err)
		}
	}
	done := make(chan struct{})
	go func() { s.requests.Wait(); close(done) }()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return closeError
	case <-timer.C:
		return errors.Join(closeError, s.timeoutError)
	}
}
