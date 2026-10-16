package ccm

import (
	"context"
	"net/http"
	"time"

	"ocm/internal/persistence"
)

type credentialStore struct {
	path     string
	client   *http.Client
	tokenURL string
	timeout  time.Duration
}

func (s *credentialStore) load(ctx context.Context) (oauthCredentials, error) {
	document, err := persistence.RefreshUnderLock(ctx, s.path, s.timeout, persistence.RefreshOps[credentialDocument]{
		Read:         func(ctx context.Context) (credentialDocument, error) { return readCredentialDocument(ctx, s.path) },
		NeedsRefresh: func(d credentialDocument) bool { return d.credentials.needsRefresh() },
		Tokens: func(d credentialDocument) (string, string) {
			return d.credentials.RefreshToken, d.credentials.AccessToken
		},
		Refresh: func(ctx context.Context, d credentialDocument) (credentialDocument, error) {
			c, err := refreshToken(ctx, s.client, d.credentials, s.tokenURL)
			return credentialDocument{credentials: c}, err
		},
		Changed: func(before, current credentialDocument) bool {
			return refreshCredentialsChanged(before.credentials, current.credentials)
		},
		Write: func(ctx context.Context, current, refreshed credentialDocument) error {
			return current.write(ctx, s.path, refreshed.credentials)
		},
	})
	return document.credentials, err
}

func refreshCredentialsChanged(before, after oauthCredentials) bool {
	return before.AccessToken != after.AccessToken || before.RefreshToken != after.RefreshToken || before.ExpiresAt != after.ExpiresAt
}
