package ocm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"ocm/internal/persistence"
)

func readCredentialsFromFile(ctx context.Context, path string) (*oauthCredentials, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var credentials *oauthCredentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, errors.New("credentials must be a JSON object")
	}
	credentials.document = data
	if err := credentials.readTokenMetadata(); err != nil {
		return nil, err
	}
	return credentials, nil
}

func (c *oauthCredentials) readTokenMetadata() error {
	if c.isAPIKeyMode() || c.Tokens == nil {
		return nil
	}
	if err := validateIdentityValue(c.Tokens.AccountID); err != nil {
		return err
	}
	claims, err := decodeCodexClaims(c.Tokens.AccessToken)
	if err != nil {
		return err
	}
	if claims != nil {
		c.identity = claims.Auth
		if claims.ExpiresAt != nil {
			expiresAt := time.Unix(*claims.ExpiresAt, 0)
			c.expiresAt = &expiresAt
		}
	}
	return nil
}

func writeCredentialsToFile(ctx context.Context, credentials *oauthCredentials, path string) error {
	data, err := credentials.encodeDocument()
	if err != nil {
		return err
	}
	return persistence.AtomicWrite(ctx, path, data, 0o600)
}

func (c *oauthCredentials) encodeDocument() ([]byte, error) {
	if len(c.document) == 0 {
		return json.MarshalIndent(c, "", "  ")
	}
	// Preserve all fields at the JSON boundary, including explicit null values.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(c.document, &fields); err != nil {
		return nil, err
	}
	if c.Tokens != nil {
		tokens, err := mergeTokenFields(fields["tokens"], c.Tokens)
		if err != nil {
			return nil, err
		}
		fields["tokens"] = tokens
	}
	if c.LastRefresh != nil {
		updated, err := json.Marshal(c.LastRefresh)
		if err != nil {
			return nil, err
		}
		fields["last_refresh"] = updated
	}
	return json.MarshalIndent(fields, "", "  ")
}

func mergeTokenFields(original json.RawMessage, tokens *tokenData) (json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	if len(original) != 0 {
		if err := json.Unmarshal(original, &fields); err != nil {
			return nil, err
		}
		if fields == nil {
			return nil, errors.New("tokens must be a JSON object")
		}
	}
	encoded, err := json.Marshal(tokens)
	if err != nil {
		return nil, err
	}
	var updated map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &updated); err != nil {
		return nil, err
	}
	for key, value := range updated {
		fields[key] = value
	}
	return json.Marshal(fields)
}
