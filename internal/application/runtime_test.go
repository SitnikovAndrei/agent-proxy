package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ocm/internal/config"
	"ocm/internal/statistics"
)

func runtimeOptions(t *testing.T) config.Application {
	t.Helper()
	dir := t.TempDir()
	ocmPath, ccmPath := filepath.Join(dir, "ocm.json"), filepath.Join(dir, "ccm.json")
	if err := os.WriteFile(ocmPath, []byte(`{"OPENAI_API_KEY":"synthetic-api-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ccmPath, []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","refreshToken":"synthetic-refresh","expiresAt":0}}`), 0600); err != nil {
		t.Fatal(err)
	}
	return config.Application{DatabasePath: filepath.Join(dir, "usage.sqlite"), OCM: &config.Config{Listen: "127.0.0.1", CredentialPath: ocmPath}, CCM: &config.Config{Listen: "127.0.0.1", CredentialPath: ccmPath}}
}

func TestRuntimeLifecycle(t *testing.T) {
	for _, mode := range []string{"ocm", "ccm", "both"} {
		t.Run(mode, func(t *testing.T) {
			options := runtimeOptions(t)
			expected := 2
			if mode == "ocm" {
				options.CCM = nil
				expected = 1
			}
			if mode == "ccm" {
				options.OCM = nil
				expected = 1
			}
			runtime, err := New(options, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := runtime.Close(); err != nil {
					t.Error(err)
				}
			})
			if len(runtime.services) != expected {
				t.Fatalf("services=%d", len(runtime.services))
			}
			if err := runtime.Start(); err != nil {
				t.Fatal(err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			_, err = runtime.statistics.Summary(context.Background(), statistics.Filter{Provider: statistics.OpenAI, Username: "test", Limit: 1})
			if err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("database still open: %v", err)
			}
		})
	}
}

type constructionService struct{ closed bool }

func (*constructionService) Start() error   { return nil }
func (s *constructionService) Close() error { s.closed = true; return nil }

func TestNewFailureClosesDatabase(t *testing.T) {
	for _, failFirst := range []bool{true, false} {
		options := runtimeOptions(t)
		failure := errors.New("synthetic construction failure")
		var captured *statistics.Store
		first := &constructionService{}
		createOCM := func(options config.Config, logger *slog.Logger, store *statistics.Store) (service, error) {
			captured = store
			if failFirst {
				return nil, failure
			}
			return first, nil
		}
		createCCM := func(options config.Config, logger *slog.Logger, store *statistics.Store) (service, error) {
			return nil, failure
		}
		runtime, err := newRuntime(options, slog.New(slog.NewTextHandler(io.Discard, nil)), createOCM, createCCM)
		if runtime != nil || !errors.Is(err, failure) {
			t.Fatalf("runtime=%v err=%v", runtime, err)
		}
		if captured == nil {
			t.Fatal("database was not opened")
		}
		_, err = captured.Summary(context.Background(), statistics.Filter{Provider: statistics.OpenAI, Username: "test", Limit: 1})
		if err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("database leaked: %v", err)
		}
		if !failFirst && !first.closed {
			t.Fatal("earlier service leaked")
		}
	}
}
