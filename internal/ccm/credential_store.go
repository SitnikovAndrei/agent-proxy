package ccm

import (
	"context"
	"errors"
	"fmt"
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

func (s *credentialStore) load(ctx context.Context) (result oauthCredentials, err error) {
	document, err := readCredentialDocument(ctx, s.path)
	if err != nil {
		return oauthCredentials{}, err
	}
	if !document.credentials.needsRefresh() {
		return document.credentials, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	lock, err := persistence.LockCredentials(ctx, s.path)
	if err != nil {
		return oauthCredentials{}, err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	return s.refresh(ctx, lock)
}

func (s *credentialStore) refresh(ctx context.Context, lock *persistence.CredentialLock) (oauthCredentials, error) {
	for {
		document, err := readCredentialDocument(ctx, s.path)
		if err != nil {
			return oauthCredentials{}, err
		}
		if !document.credentials.needsRefresh() {
			return document.credentials, nil
		}
		if err := lock.BeginRotation(ctx, document.credentials.RefreshToken, document.credentials.AccessToken); err != nil {
			return oauthCredentials{}, err
		}
		refreshed, err := refreshToken(ctx, s.client, document.credentials, s.tokenURL)
		if err != nil {
			return oauthCredentials{}, err
		}
		persistContext := context.WithoutCancel(ctx)
		current, err := readCredentialDocument(persistContext, s.path)
		if err != nil {
			return oauthCredentials{}, err
		}
		if refreshCredentialsChanged(document.credentials, current.credentials) {
			continue
		}
		if err := current.write(persistContext, s.path, refreshed); err != nil {
			return oauthCredentials{}, fmt.Errorf("persist refreshed credentials: %w", err)
		}
		if err := lock.CompleteRotation(); err != nil {
			return oauthCredentials{}, fmt.Errorf("commit credential rotation: %w", err)
		}
		current, err = readCredentialDocument(persistContext, s.path)
		return current.credentials, err
	}
}

func refreshCredentialsChanged(before, after oauthCredentials) bool {
	return before.AccessToken != after.AccessToken || before.RefreshToken != after.RefreshToken || before.ExpiresAt != after.ExpiresAt
}
