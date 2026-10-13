package ocm

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

const bufferSize = 32 * 1024

type Options struct {
	config.Config
	openaiAPIBaseURL  string
	chatGPTBackendURL string
	oauth2TokenURL    string
	refreshTimeout    time.Duration
}

type Service struct {
	ctx               context.Context
	cancel            context.CancelFunc
	logger            *slog.Logger
	credentialStore   *credentialStore
	users             []config.User
	dialer            *net.Dialer
	httpClient        *http.Client
	httpHeaders       http.Header
	listener          net.Listener
	httpServer        *http.Server
	options           Options
	userManager       *proxy.UserManager
	usageTracker      *usageTracker
	statistics        *statistics.Handler
	webSocketMutex    sync.Mutex
	webSocketConns    map[*webSocketSession]struct{}
	shuttingDown      bool
	requestGroup      sync.WaitGroup
	closeOnce         sync.Once
	closeError        error
	openaiAPIBaseURL  string
	chatGPTBackendURL string
}

func NewService(opts Options, logger *slog.Logger, store *statistics.Store) (*Service, error) {
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if store == nil {
		return nil, errors.New("statistics store is required")
	}
	path, err := ResolveCredentialPath(opts.CredentialPath)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	client := proxy.NewHTTPClient(dialer)
	s := &Service{
		ctx: ctx, cancel: cancel, logger: logger, options: opts,
		users: opts.Users, dialer: dialer, httpClient: client,
		credentialStore: createCredentialStore(path, client, opts),
		httpHeaders:     opts.Headers.Build(), userManager: &proxy.UserManager{},
		webSocketConns:   make(map[*webSocketSession]struct{}),
		openaiAPIBaseURL: openaiAPIBaseURL, chatGPTBackendURL: chatGPTBackendURL,
	}
	if opts.openaiAPIBaseURL != "" {
		s.openaiAPIBaseURL = opts.openaiAPIBaseURL
	}
	if opts.chatGPTBackendURL != "" {
		s.chatGPTBackendURL = opts.chatGPTBackendURL
	}
	s.usageTracker = &usageTracker{store: store}
	s.statistics, err = statistics.NewHandler(store, s.userManager, statistics.OpenAI, logger)
	if err != nil {
		cancel()
		return nil, err
	}

	return s, nil
}

func (s *Service) Start() error {
	s.userManager.UpdateUsers(s.users)
	if _, err := s.credentialStore.read(s.ctx); err != nil {
		return fmt.Errorf("read credentials: %w", err)
	}
	listener, err := proxy.Listen(s.options.Config)
	if err != nil {
		return err
	}
	s.listener = listener
	s.httpServer = &http.Server{Handler: s, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("serve error", "error", err)
		}
	}()
	s.logger.Info("listening on " + listener.Addr().String())
	return nil
}

func (s *Service) baseURL(credentials *oauthCredentials) string {
	if credentials.isAPIKeyMode() {
		return s.openaiAPIBaseURL
	}
	return s.chatGPTBackendURL
}

func createCredentialStore(path string, client *http.Client, opts Options) *credentialStore {
	tokenURL := oauth2TokenURL
	if opts.oauth2TokenURL != "" {
		tokenURL = opts.oauth2TokenURL
	}
	timeout := tokenRefreshTimeout
	if opts.refreshTimeout != 0 {
		timeout = opts.refreshTimeout
	}
	return newCredentialStore(path, client, tokenURL, timeout)
}
