package ccm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"ocm/internal/config"
	"ocm/internal/proxy"
	"ocm/internal/statistics"
)

type Options struct {
	config.Config
	apiBaseURL     string
	tokenURL       string
	refreshTimeout time.Duration
}

type Service struct {
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *slog.Logger
	options      Options
	credentials  *credentialStore
	users        *proxy.UserManager
	client       *http.Client
	headers      http.Header
	baseURL      string
	listener     net.Listener
	server       *http.Server
	usage        *usageTracker
	statistics   *statistics.Handler
	requestMutex sync.Mutex
	requests     sync.WaitGroup
	stopping     bool
	closeOnce    sync.Once
	closeError   error
}

func NewService(options Options, logger *slog.Logger, store *statistics.Store) (*Service, error) {
	if err := options.Config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if store == nil {
		return nil, errors.New("statistics store is required")
	}
	path, err := ResolveCredentialPath(options.CredentialPath)
	if err != nil {
		return nil, err
	}
	client := proxy.NewHTTPClient(&net.Dialer{Timeout: 30 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{ctx: ctx, cancel: cancel, logger: logger, options: options, client: client, headers: options.Headers.Build(), baseURL: claudeAPIBaseURL, users: &proxy.UserManager{}}
	tokenURL := oauth2TokenURL
	if options.tokenURL != "" {
		tokenURL = options.tokenURL
	}
	timeout := tokenRefreshTimeout
	if options.refreshTimeout != 0 {
		timeout = options.refreshTimeout
	}
	service.credentials = &credentialStore{path: path, client: client, tokenURL: tokenURL, timeout: timeout}
	if options.apiBaseURL != "" {
		service.baseURL = options.apiBaseURL
	}
	service.usage = &usageTracker{store: store}
	service.statistics, err = statistics.NewHandler(store, service.users, statistics.Anthropic, logger)
	if err != nil {
		cancel()
		return nil, err
	}

	return service, nil
}

func (s *Service) Start() error {
	if _, err := readCredentialDocument(s.ctx, s.credentials.path); err != nil {
		return fmt.Errorf("read Claude credentials: %w", err)
	}
	s.users.UpdateUsers(s.options.Users)
	listener, err := proxy.Listen(s.options.Config)
	if err != nil {
		return err
	}
	s.listener = listener
	s.server = &http.Server{Handler: s, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("serve error", "error", err)
		}
	}()
	s.logger.Info("listening", "address", listener.Addr().String())
	return nil
}

func (s *Service) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.requestMutex.Lock()
	if s.stopping {
		s.requestMutex.Unlock()
		writeJSONError(writer, request, http.StatusServiceUnavailable, "api_error", "service is shutting down")
		return
	}
	s.requests.Add(1)
	s.requestMutex.Unlock()
	defer s.requests.Done()
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	s.serveRequest(writer, request.WithContext(ctx))
}

func (s *Service) Close() error {
	s.closeOnce.Do(func() { s.closeError = s.close() })
	return s.closeError
}

func (s *Service) close() error {
	s.requestMutex.Lock()
	s.stopping = true
	s.requestMutex.Unlock()
	s.cancel()
	defer s.client.CloseIdleConnections()
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
	case <-timer.C:
		return errors.Join(closeError, errors.New("Claude HTTP handlers did not stop within 30 seconds"))
	}

	return closeError
}
