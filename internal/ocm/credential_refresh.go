package ocm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type oauthRefreshRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
	Scope        string `json:"scope"`
}

type oauthRefreshResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func refreshToken(ctx context.Context, client *http.Client, credentials *oauthCredentials, tokenURL string) (*oauthCredentials, error) {
	if credentials.Tokens == nil || credentials.Tokens.RefreshToken == "" {
		return nil, errors.New("refresh token is empty")
	}
	request, err := newRefreshRequest(ctx, credentials.Tokens.RefreshToken, tokenURL)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed: HTTP %d", response.StatusCode)
	}
	return decodeRefreshResponse(response.Body, credentials)
}

func newRefreshRequest(ctx context.Context, refreshToken, tokenURL string) (*http.Request, error) {
	payload := oauthRefreshRequest{
		GrantType: "refresh_token", RefreshToken: refreshToken,
		ClientID: oauth2ClientID, Scope: "openid profile email",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal refresh request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	return request, nil
}

func decodeRefreshResponse(body io.Reader, credentials *oauthCredentials) (*oauthCredentials, error) {
	var token oauthRefreshResponse
	if err := json.NewDecoder(body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	updated := *credentials
	updatedTokens := *credentials.Tokens
	updated.Tokens = &updatedTokens
	if token.IDToken != "" {
		updated.Tokens.IDToken = token.IDToken
	}
	if token.AccessToken != "" {
		updated.Tokens.AccessToken = token.AccessToken
	}
	if token.RefreshToken != "" {
		updated.Tokens.RefreshToken = token.RefreshToken
	}
	now := time.Now()
	updated.LastRefresh = &now
	return &updated, nil
}
