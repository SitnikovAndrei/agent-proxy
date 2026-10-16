package ccm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"ocm/internal/persistence"
)

const (
	oauth2ClientID          = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	oauth2TokenURL          = "https://console.anthropic.com/v1/oauth/token"
	claudeAPIBaseURL        = "https://api.anthropic.com"
	anthropicBetaOAuthValue = "oauth-2025-04-20"
	tokenRefreshTimeout     = 30 * time.Second
)

type oauthCredentials struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"`
	Scopes           []string `json:"scopes,omitempty"`
	SubscriptionType string   `json:"subscriptionType,omitempty"`
	IsMax            bool     `json:"isMax,omitempty"`
}

func (c oauthCredentials) needsRefresh() bool {
	return c.ExpiresAt != 0 && time.Now().UnixMilli() >= c.ExpiresAt-60000
}

func defaultCredentialPath() (string, error) {
	if directory := os.Getenv("CLAUDE_CONFIG_DIR"); directory != "" {
		return filepath.Join(directory, ".credentials.json"), nil
	}
	var account *user.User
	var err error
	if name := os.Getenv("SUDO_USER"); name != "" {
		account, err = user.Lookup(name)
		if err != nil {
			return "", fmt.Errorf("resolve SUDO_USER: %w", err)
		}
	} else {
		account, err = user.Current()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(account.HomeDir, ".claude", ".credentials.json"), nil
}

func ResolveCredentialPath(path string) (string, error) {
	if path == "" {
		var err error
		path, err = defaultCredentialPath()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(path)
}

func refreshToken(ctx context.Context, client *http.Client, credentials oauthCredentials, tokenURL string) (oauthCredentials, error) {
	if credentials.RefreshToken == "" {
		return oauthCredentials{}, errors.New("refresh token is empty")
	}
	payload := struct {
		GrantType    string `json:"grant_type"`
		RefreshToken string `json:"refresh_token"`
		ClientID     string `json:"client_id"`
	}{"refresh_token", credentials.RefreshToken, oauth2ClientID}
	body, err := json.Marshal(payload)
	if err != nil {
		return oauthCredentials{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return oauthCredentials{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return oauthCredentials{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest && response.StatusCode < http.StatusInternalServerError {
		return oauthCredentials{}, fmt.Errorf("refresh failed: HTTP %d: %w", response.StatusCode, persistence.ErrRefreshRejected)
	}
	if response.StatusCode != http.StatusOK {
		return oauthCredentials{}, fmt.Errorf("refresh failed: HTTP %d", response.StatusCode)
	}
	return decodeRefreshedToken(response.Body, credentials)
}

func decodeRefreshedToken(body io.Reader, credentials oauthCredentials) (oauthCredentials, error) {
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(body).Decode(&token); err != nil {
		return oauthCredentials{}, fmt.Errorf("decode token response: %w", err)
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 {
		return oauthCredentials{}, errors.New("invalid refreshed token or expiration")
	}
	credentials.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		credentials.RefreshToken = token.RefreshToken
	}
	credentials.ExpiresAt = time.Now().UnixMilli() + token.ExpiresIn*1000
	return credentials, nil
}
