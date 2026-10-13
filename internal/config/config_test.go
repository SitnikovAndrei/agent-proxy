package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		valid      bool
	}{
		{"defaults", `{}`, true},
		{"headers", `{"headers":{"X-One":"one","X-Many":["a","b"]},"users":[{"name":"test","token":"key"}]}`, true},
		{"unknown root", `{"typo":true}`, false},
		{"unknown tls", `{"tls":{"typo":true}}`, false},
		{"unknown user", `{"users":[{"token":"key","typo":true}]}`, false},
		{"missing token", `{"users":[{"name":"test"}]}`, false},
		{"duplicate token", `{"users":[{"token":"key"},{"token":"key"}]}`, false},
		{"tls missing paths", `{"tls":{"enabled":true}}`, false},
		{"bad level", `{"log_level":"verbose"}`, false},
		{"bad header", `{"headers":{"X-Bad":1}}`, false},
		{"null header", `{"headers":{"X-Bad":null}}`, false},
		{"null header item", `{"headers":{"X-Bad":[null]}}`, false},
		{"trailing json", `{} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if !tc.valid {
				return
			}
			if cfg.OCM.Listen != "127.0.0.1" || cfg.OCM.ListenPort != 8080 || cfg.LogLevel != "info" {
				t.Fatalf("incorrect defaults: %+v", cfg)
			}
			if tc.name == "headers" {
				header := cfg.OCM.Headers.Build()
				if header.Get("X-One") != "one" || len(header.Values("X-Many")) != 2 {
					t.Fatalf("incorrect headers: %v", header)
				}
			}
		})
	}
}
