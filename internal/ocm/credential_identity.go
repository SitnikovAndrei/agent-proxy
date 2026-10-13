package ocm

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type codexClaims struct {
	ExpiresAt *int64         `json:"exp"`
	Auth      *codexIdentity `json:"https://api.openai.com/auth"`
}

type codexIdentity struct {
	AccountID        *string `json:"chatgpt_account_id"`
	DataResidency    *string `json:"chatgpt_data_residency"`
	ComputeResidency *string `json:"chatgpt_compute_residency"`
}

func decodeCodexClaims(accessToken string) (*codexClaims, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) == 1 {
		// Opaque access tokens contain no local workspace metadata.
		return nil, nil
	}
	if len(parts) != 3 {
		return nil, errors.New("OAuth JWT must have three segments")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("OAuth JWT payload has invalid base64 encoding")
	}
	var claims *codexClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.New("OAuth JWT claims have invalid JSON or field types")
	}
	if claims == nil {
		return nil, errors.New("OAuth JWT payload must be a JSON object")
	}
	if claims.ExpiresAt != nil && *claims.ExpiresAt <= 0 {
		return nil, errors.New("OAuth JWT exp must be a positive Unix timestamp")
	}
	if claims.Auth != nil {
		if err := claims.Auth.validate(); err != nil {
			return nil, err
		}
	}
	return claims, nil
}

func (i codexIdentity) validate() error {
	for _, value := range []*string{i.AccountID, i.DataResidency, i.ComputeResidency} {
		if value == nil {
			continue
		}
		if *value == "" {
			return errors.New("OAuth JWT workspace identity fields must not be empty")
		}
		if err := validateIdentityValue(*value); err != nil {
			return err
		}
	}
	return nil
}

func validateIdentityValue(value string) error {
	if strings.TrimSpace(value) != value {
		return errors.New("workspace identity contains surrounding whitespace")
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return errors.New("workspace identity contains a control character")
		}
	}
	return nil
}

func (c *oauthCredentials) getResidency() *string {
	if c.isAPIKeyMode() || c.identity == nil {
		return nil
	}
	if c.identity.DataResidency != nil {
		return c.identity.DataResidency
	}
	return c.identity.ComputeResidency
}
