package ocm

import (
	"context"
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

func (s *credentialStore) load(ctx context.Context) (*oauthCredentials, error) {
	return persistence.RefreshUnderLock(ctx, s.path, s.timeout, persistence.RefreshOps[*oauthCredentials]{
		Read:         s.read,
		NeedsRefresh: func(c *oauthCredentials) bool { return c.needsRefresh() },
		Tokens:       func(c *oauthCredentials) (string, string) { return c.Tokens.RefreshToken, c.Tokens.AccessToken },
		Refresh: func(ctx context.Context, c *oauthCredentials) (*oauthCredentials, error) {
			return refreshToken(ctx, s.client, c, s.tokenURL)
		},
		Changed: func(before, current *oauthCredentials) bool { return !credentialsEqual(before, current) },
		Write: func(ctx context.Context, current, refreshed *oauthCredentials) error {
			refreshed.document = current.document
			return writeCredentialsToFile(ctx, refreshed, s.path)
		},
	})
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
