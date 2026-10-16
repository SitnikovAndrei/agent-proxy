package application

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"ocm/internal/ccm"
	"ocm/internal/config"
	"ocm/internal/ocm"
	"ocm/internal/statistics"
)

type service interface {
	Start() error
	Close() error
}

type namedService struct {
	name    string
	service service
}

type Runtime struct {
	services   []namedService
	statistics *statistics.Store
	closeOnce  sync.Once
	closeError error
}

type serviceFactory func(config.Config, *slog.Logger, *statistics.Store) (service, error)

func New(options config.Application, logger *slog.Logger) (*Runtime, error) {
	return newRuntime(options, logger,
		func(options config.Config, logger *slog.Logger, store *statistics.Store) (service, error) {
			return ocm.NewService(ocm.Options{Config: options}, logger, store)
		},
		func(options config.Config, logger *slog.Logger, store *statistics.Store) (service, error) {
			return ccm.NewService(ccm.Options{Config: options}, logger, store)
		})
}

func newRuntime(options config.Application, logger *slog.Logger, createOCM, createCCM serviceFactory) (*Runtime, error) {
	var err error
	options, err = resolveCredentials(options)
	if err != nil {
		return nil, err
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	store, err := statistics.Open(options.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open statistics database: %w", err)
	}
	runtime := &Runtime{statistics: store}
	if options.OCM != nil {
		service, err := createOCM(*options.OCM, logger.With("service", "ocm"), store)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("create ocm: %w", err), runtime.Close())
		}
		runtime.services = append(runtime.services, namedService{"ocm", service})
	}
	if options.CCM != nil {
		service, err := createCCM(*options.CCM, logger.With("service", "ccm"), store)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("create ccm: %w", err), runtime.Close())
		}
		runtime.services = append(runtime.services, namedService{"ccm", service})
	}
	return runtime, nil
}

func (r *Runtime) Start() error {
	for _, service := range r.services {
		if err := service.service.Start(); err != nil {
			return errors.Join(fmt.Errorf("start %s: %w", service.name, err), r.Close())
		}
	}
	return nil
}

func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		results := make(chan error, len(r.services))
		for _, service := range r.services {
			go func() {
				err := service.service.Close()
				if err != nil {
					err = fmt.Errorf("close %s: %w", service.name, err)
				}
				results <- err
			}()
		}
		for range r.services {
			r.closeError = errors.Join(r.closeError, <-results)
		}
		if r.statistics != nil {
			r.closeError = errors.Join(r.closeError, r.statistics.Close())
		}
	})
	return r.closeError
}

func resolveCredentials(options config.Application) (config.Application, error) {
	if options.OCM != nil {
		endpoint := *options.OCM
		path, err := ocm.ResolveCredentialPath(endpoint.CredentialPath)
		if err != nil {
			return config.Application{}, fmt.Errorf("resolve ocm credentials: %w", err)
		}
		endpoint.CredentialPath = path
		options.OCM = &endpoint
	}
	if options.CCM != nil {
		endpoint := *options.CCM
		path, err := ccm.ResolveCredentialPath(endpoint.CredentialPath)
		if err != nil {
			return config.Application{}, fmt.Errorf("resolve ccm credentials: %w", err)
		}
		endpoint.CredentialPath = path
		options.CCM = &endpoint
	}
	return options, nil
}
