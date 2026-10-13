package ocm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"ocm/internal/persistence"
)

const tokenRefreshTimeout = 30 * time.Second

type credentialStore struct {
	path     string
	client   *http.Client
	tokenURL string
	timeout  time.Duration
}

func newCredentialStore(path string, client *http.Client, tokenURL string, timeout time.Duration) *credentialStore {
	return &credentialStore{path: path, client: client, tokenURL: tokenURL, timeout: timeout}
}

func ResolveCredentialPath(path string) (string, error) {
	if path == "" {
		var err error
		path, err = getDefaultCredentialsPath()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(path)
}

func (s *credentialStore) read(ctx context.Context) (*oauthCredentials, error) {
	return readCredentialsFromFile(ctx, s.path)
}

func (s *credentialStore) load(ctx context.Context) (result *oauthCredentials, err error) {
	credentials, err := s.read(ctx)
	if err != nil || !credentials.needsRefresh() {
		return credentials, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	lock, err := persistence.LockCredentials(ctx, s.path)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	return s.refresh(ctx, lock)
}

func (s *credentialStore) refresh(ctx context.Context, lock *persistence.CredentialLock) (*oauthCredentials, error) {
	for {
		credentials, err := s.read(ctx)
		if err != nil || !credentials.needsRefresh() {
			return credentials, err
		}
		if err := lock.BeginRotation(ctx, credentials.Tokens.RefreshToken, credentials.Tokens.AccessToken); err != nil {
			return nil, err
		}
		refreshed, err := refreshToken(ctx, s.client, credentials, s.tokenURL)
		if err != nil {
			return nil, err
		}
		// A successful refresh rotates tokens; persist them even if the caller just canceled.
		persistContext := context.WithoutCancel(ctx)
		current, err := readCredentialsFromFile(persistContext, s.path)
		if err != nil {
			return nil, err
		}
		if !credentialsEqual(credentials, current) {
			continue
		}
		// Preserve metadata changed during refresh without rotating the token again.
		refreshed.document = current.document
		if err := writeCredentialsToFile(persistContext, refreshed, s.path); err != nil {
			return nil, fmt.Errorf("persist refreshed credentials: %w", err)
		}
		if err := lock.CompleteRotation(); err != nil {
			return nil, fmt.Errorf("commit credential rotation: %w", err)
		}
		return s.read(persistContext)
	}
}

func credentialsEqual(left, right *oauthCredentials) bool {
	if left.APIKey != right.APIKey || (left.Tokens == nil) != (right.Tokens == nil) {
		return false
	}
	if left.Tokens != nil && *left.Tokens != *right.Tokens {
		return false
	}
	if left.LastRefresh == nil || right.LastRefresh == nil {
		return left.LastRefresh == right.LastRefresh
	}
	return left.LastRefresh.Equal(*right.LastRefresh)
}
