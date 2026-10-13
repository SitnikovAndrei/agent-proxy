package ocm

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

const (
	oauth2ClientID           = "app_EMoamEEZ73f0CkXaXp7hrann"
	oauth2TokenURL           = "https://auth.openai.com/oauth/token"
	openaiAPIBaseURL         = "https://api.openai.com"
	chatGPTBackendURL        = "https://chatgpt.com/backend-api/codex"
	tokenRefreshIntervalDays = 8
	tokenRefreshSkew         = 120 * time.Second
)

func getRealUser() (*user.User, error) {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		sudoUserInfo, err := user.Lookup(sudoUser)
		if err == nil {
			return sudoUserInfo, nil
		}
	}
	return user.Current()
}

func getDefaultCredentialsPath() (string, error) {
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		return filepath.Join(codexHome, "auth.json"), nil
	}
	userInfo, err := getRealUser()
	if err != nil {
		return "", err
	}
	return filepath.Join(userInfo.HomeDir, ".codex", "auth.json"), nil
}

type oauthCredentials struct {
	APIKey      string     `json:"OPENAI_API_KEY,omitempty"`
	Tokens      *tokenData `json:"tokens,omitempty"`
	LastRefresh *time.Time `json:"last_refresh,omitempty"`
	document    json.RawMessage
	identity    *codexIdentity
	expiresAt   *time.Time
}

type tokenData struct {
	IDToken      string `json:"id_token,omitempty"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id,omitempty"`
}

func (c *oauthCredentials) isAPIKeyMode() bool {
	return c.APIKey != ""
}

func (c *oauthCredentials) getAccessToken() string {
	if c.APIKey != "" {
		return c.APIKey
	}
	if c.Tokens != nil {
		return c.Tokens.AccessToken
	}
	return ""
}

func (c *oauthCredentials) getAccountID() string {
	if c.isAPIKeyMode() {
		return ""
	}
	if c.Tokens != nil && c.Tokens.AccountID != "" {
		return c.Tokens.AccountID
	}
	if c.identity != nil && c.identity.AccountID != nil {
		return *c.identity.AccountID
	}
	return ""
}

func (c *oauthCredentials) needsRefresh() bool {
	if c.APIKey != "" {
		return false
	}
	if c.Tokens == nil || c.Tokens.RefreshToken == "" {
		return false
	}
	if c.expiresAt != nil {
		return !time.Now().Add(tokenRefreshSkew).Before(*c.expiresAt)
	}
	if c.LastRefresh == nil {
		return true
	}
	return time.Since(*c.LastRefresh) >= time.Duration(tokenRefreshIntervalDays)*24*time.Hour
}
