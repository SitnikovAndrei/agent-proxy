package ccm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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
	lifecycle   *proxy.Lifecycle
	ctx         context.Context
	logger      *slog.Logger
	options     Options
	credentials *credentialStore
	users       *proxy.UserManager
	client      *http.Client
	headers     http.Header
	baseURL     string
	listener    net.Listener
	usage       *usageTracker
	statistics  *statistics.Handler
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
	service := &Service{ctx: ctx, logger: logger, options: options, client: client, headers: options.Headers.Build(), baseURL: claudeAPIBaseURL, users: &proxy.UserManager{}}
	service.lifecycle = proxy.NewLifecycle(ctx, cancel, logger, client, errors.New("Claude HTTP handlers did not stop within 30 seconds"))
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
	var err error
	s.listener, err = s.lifecycle.Start(s.options.Config, s, func(address net.Addr) { s.logger.Info("listening", "address", address.String()) })
	return err
}

func (s *Service) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.lifecycle.ServeHTTP(writer, request, s.serveRequest, func(w http.ResponseWriter, r *http.Request) {
		writeJSONError(w, r, http.StatusServiceUnavailable, "api_error", "service is shutting down")
	})
}

func (s *Service) Close() error { return s.lifecycle.Close() }
