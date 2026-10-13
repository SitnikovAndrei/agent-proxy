package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type User struct {
	Name  string `json:"name"`
	Token string `json:"token"`
}

type TLS struct {
	Enabled         bool   `json:"enabled"`
	CertificatePath string `json:"certificate_path"`
	KeyPath         string `json:"key_path"`
}

type Headers map[string][]string

func (h *Headers) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	result := make(Headers, len(raw))
	for key, value := range raw {
		var single string
		if len(value) > 0 && value[0] == '"' {
			if err := json.Unmarshal(value, &single); err != nil {
				return err
			}
			result[key] = []string{single}
		} else {
			var values []string
			if len(value) == 0 || value[0] != '[' {
				return fmt.Errorf("header %q must be a string or an array of strings", key)
			}
			var items []json.RawMessage
			if err := json.Unmarshal(value, &items); err != nil {
				return err
			}
			for _, item := range items {
				if len(item) == 0 || item[0] != '"' {
					return fmt.Errorf("header %q must contain strings", key)
				}
			}
			if err := json.Unmarshal(value, &values); err != nil {
				return fmt.Errorf("header %q: %w", key, err)
			}
			result[key] = values
		}
	}
	*h = result
	return nil
}

func (h Headers) Build() http.Header {
	result := make(http.Header, len(h))
	for key, values := range h {
		result[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	return result
}

type Config struct {
	LogLevel       string  `json:"log_level"`
	Listen         string  `json:"listen"`
	ListenPort     int     `json:"listen_port"`
	TLS            TLS     `json:"tls"`
	CredentialPath string  `json:"credential_path"`
	Users          []User  `json:"users"`
	Headers        Headers `json:"headers"`
}

func (c Config) Validate() error {
	switch c.LogLevel {
	case "", "debug", "info", "warn", "error":
	default:
		return errors.New("log_level must be debug, info, warn or error")
	}
	if c.ListenPort < 0 || c.ListenPort > 65535 {
		return errors.New("listen_port must be between 0 and 65535")
	}
	tokens := make(map[string]struct{}, len(c.Users))
	for _, user := range c.Users {
		if strings.TrimSpace(user.Token) == "" {
			return errors.New("user token is required")
		}
		if _, exists := tokens[user.Token]; exists {
			return errors.New("user tokens must be unique")
		}
		tokens[user.Token] = struct{}{}
	}
	if c.TLS.Enabled && (c.TLS.CertificatePath == "" || c.TLS.KeyPath == "") {
		return errors.New("TLS certificate_path and key_path are required")
	}
	return nil
}
