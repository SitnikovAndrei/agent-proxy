package ccm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"ocm/internal/persistence"
)

// Unknown fields belong to Claude Code and must survive token rotation.
type credentialDocument struct {
	credentials oauthCredentials
	fields      map[string]json.RawMessage
	oauthFields map[string]json.RawMessage
}

func readCredentialDocument(ctx context.Context, path string) (credentialDocument, error) {
	if err := ctx.Err(); err != nil {
		return credentialDocument{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return credentialDocument{}, err
	}
	var container struct {
		OAuth *oauthCredentials `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &container); err != nil {
		return credentialDocument{}, err
	}
	if container.OAuth == nil {
		return credentialDocument{}, errors.New("claudeAiOauth field not found in credentials")
	}
	if strings.TrimSpace(container.OAuth.AccessToken) == "" {
		return credentialDocument{}, errors.New("accessToken is empty")
	}
	if container.OAuth.ExpiresAt < 0 {
		return credentialDocument{}, errors.New("expiresAt must not be negative")
	}
	document := credentialDocument{credentials: *container.OAuth}
	if err := json.Unmarshal(data, &document.fields); err != nil {
		return credentialDocument{}, err
	}
	if err := json.Unmarshal(document.fields["claudeAiOauth"], &document.oauthFields); err != nil {
		return credentialDocument{}, err
	}
	return document, nil
}

func (d credentialDocument) write(ctx context.Context, path string, refreshed oauthCredentials) error {
	tokens := struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	}{refreshed.AccessToken, refreshed.RefreshToken, refreshed.ExpiresAt}
	raw, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	var changed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &changed); err != nil {
		return err
	}
	for key, value := range changed {
		d.oauthFields[key] = value
	}
	oauth, err := json.Marshal(d.oauthFields)
	if err != nil {
		return err
	}
	d.fields["claudeAiOauth"] = oauth
	data, err := json.MarshalIndent(d.fields, "", "  ")
	if err != nil {
		return err
	}
	return persistence.AtomicWrite(ctx, path, data, 0o600)
}
