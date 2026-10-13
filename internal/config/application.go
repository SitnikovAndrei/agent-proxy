package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"ocm/internal/persistence"
)

type Application struct {
	LogLevel     string
	DatabasePath string
	OCM          *Config
	CCM          *Config
}

func Load(path string) (Application, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Application{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Application{}, err
	}
	if fields == nil {
		return Application{}, errors.New("configuration must be an object")
	}
	_, hasOCM := fields["ocm"]
	_, hasCCM := fields["ccm"]
	if !hasOCM && !hasCCM {
		wire := struct {
			Config
			DatabasePath string `json:"database_path"`
		}{Config: defaultEndpoint(8080), DatabasePath: "usage.sqlite"}
		if err := decodeConfig(data, &wire); err != nil {
			return Application{}, err
		}
		app := Application{LogLevel: wire.LogLevel, DatabasePath: wire.DatabasePath, OCM: &wire.Config}
		return app, app.Validate()
	}
	return loadApplication(data)
}

func defaultEndpoint(port int) Config {
	return Config{LogLevel: "info", Listen: "127.0.0.1", ListenPort: port}
}

func decodeConfig(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("config must contain one JSON object")
	}
	return nil
}

func loadApplication(data []byte) (Application, error) {
	wire := struct {
		LogLevel     string          `json:"log_level"`
		DatabasePath string          `json:"database_path"`
		OCM          json.RawMessage `json:"ocm"`
		CCM          json.RawMessage `json:"ccm"`
	}{LogLevel: "info", DatabasePath: "usage.sqlite"}
	if err := decodeConfig(data, &wire); err != nil {
		return Application{}, err
	}
	app := Application{LogLevel: wire.LogLevel, DatabasePath: wire.DatabasePath}
	for _, service := range []struct {
		name   string
		raw    json.RawMessage
		port   int
		target **Config
	}{
		{"ocm", wire.OCM, 8080, &app.OCM}, {"ccm", wire.CCM, 8081, &app.CCM},
	} {
		if len(service.raw) == 0 {
			continue
		}
		endpoint, err := parseEndpoint(service.name, service.raw, service.port)
		if err != nil {
			return Application{}, err
		}
		*service.target = &endpoint
	}
	return app, app.Validate()
}

func (a Application) Validate() error {
	if strings.TrimSpace(a.DatabasePath) == "" {
		return errors.New("database_path must not be empty")
	}
	if err := (Config{LogLevel: a.LogLevel}).Validate(); err != nil {
		return err
	}
	if a.OCM == nil && a.CCM == nil {
		return errors.New("at least one of ocm or ccm is required")
	}
	for _, service := range []struct {
		name   string
		config *Config
	}{{"ocm", a.OCM}, {"ccm", a.CCM}} {
		if service.config != nil {
			if err := service.config.Validate(); err != nil {
				return fmt.Errorf("%s: %w", service.name, err)
			}
		}
	}
	if a.OCM != nil && a.CCM != nil {
		if a.OCM.ListenPort != 0 && a.OCM.ListenPort == a.CCM.ListenPort && listenersOverlap(a.OCM.Listen, a.CCM.Listen) {
			return errors.New("ocm and ccm listening addresses overlap")
		}
	}
	paths := []string{a.DatabasePath, a.DatabasePath + "-wal", a.DatabasePath + "-shm"}
	for _, endpoint := range []*Config{a.OCM, a.CCM} {
		if endpoint != nil {
			paths = append(paths, endpoint.CredentialPath)
			if endpoint.CredentialPath != "" {
				lockPath, err := persistence.CredentialLockPath(endpoint.CredentialPath)
				if err != nil {
					return fmt.Errorf("resolve credential lock path: %w", err)
				}
				paths = append(paths, lockPath)
			}
		}
	}
	return ValidateFileSeparation(paths...)
}

func listenersOverlap(left, right string) bool {
	wildcard := func(host string) bool { return host == "" || host == "0.0.0.0" || host == "::" }
	return strings.EqualFold(left, right) || wildcard(left) || wildcard(right)
}

func parseEndpoint(name string, raw json.RawMessage, port int) (Config, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return Config{}, fmt.Errorf("%s must be a configuration object", name)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Config{}, err
	}
	if _, present := fields["database_path"]; present {
		return Config{}, fmt.Errorf("%s.database_path is not supported; configure database_path at the root", name)
	}
	if _, present := fields["log_level"]; present {
		return Config{}, fmt.Errorf("%s.log_level is not supported; configure log_level at the root", name)
	}
	endpoint := defaultEndpoint(port)
	if err := decodeConfig(raw, &endpoint); err != nil {
		return Config{}, fmt.Errorf("%s: %w", name, err)
	}
	return endpoint, nil
}
