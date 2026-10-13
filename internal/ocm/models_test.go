package ocm

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestModelsHTTPCompatibility(t *testing.T) {
	const catalog = `{"models":[{"slug":"test-model","supports_reasoning_effort_updates":false,"default_reasoning_level":"high"}],"extra":null}`
	for _, test := range []struct {
		name    string
		oauth   bool
		query   string
		version string
		status  int
		body    string
	}{
		{"oauth-default-version", true, "?other=value", codexCatalogClientVersion, http.StatusOK, catalog},
		{"oauth-explicit-version", true, "?client_version=custom&other=value", "custom", http.StatusOK, catalog},
		{"oauth-empty-version", true, "?client_version=&other=value", "", http.StatusOK, catalog},
		{"oauth-upstream-error", true, "?other=value", codexCatalogClientVersion, http.StatusForbidden, `{"error":"denied"}`},
		{"api-key-passthrough", false, "?other=value", "", http.StatusOK, `{"object":"list","data":[{"id":"native"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path, token := "/v1/models", "Bearer upstream-key"
				if test.oauth {
					path, token = "/models", "Bearer access-token"
					if !r.URL.Query().Has("client_version") || r.URL.Query().Get("client_version") != test.version {
						t.Errorf("unexpected catalog version: %s", r.URL.RawQuery)
					}
					if r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("ChatGPT-Account-Id") != "account-id" {
						t.Errorf("unexpected OAuth headers: %v", r.Header)
					}
				} else if r.URL.Query().Has("client_version") {
					t.Error("API key request acquired a catalog version")
				}
				if r.Method != http.MethodGet || r.URL.Path != path || r.URL.Query().Get("other") != "value" || r.Header.Get("Authorization") != token {
					t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Length", strconv.Itoa(len(test.body)))
				w.Header().Set("ETag", `"upstream-tag"`)
				w.Header().Set("Content-MD5", "upstream-checksum")
				w.Header().Set("Digest", "sha-256=upstream-checksum")
				w.Header().Set("X-Catalog-Metadata", "preserved")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer upstream.Close()
			service := startTestService(t, upstream.URL, test.oauth, io.Discard)
			request, err := http.NewRequest(http.MethodGet, "http://"+service.listener.Addr().String()+"/v1/models"+test.query, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer client-key")
			client := &http.Client{Timeout: 5 * time.Second}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.status || response.Header.Get("X-Catalog-Metadata") != "preserved" {
				t.Fatalf("unexpected response: %d %v %s", response.StatusCode, response.Header, body)
			}
			if test.oauth && test.status == http.StatusOK {
				for _, header := range []string{"ETag", "Content-MD5", "Digest", "Content-Encoding"} {
					if response.Header.Get(header) != "" {
						t.Errorf("transformed response retains %s", header)
					}
				}
				assertCatalogJSONEqual(t, body, []byte(`{"models":[{"slug":"test-model","supports_reasoning_effort_updates":false,"default_reasoning_level":"high"}],"extra":null,"object":"list","data":[{"id":"test-model","object":"model","created":0,"owned_by":"openai"}]}`))
			} else {
				if string(body) != test.body || response.Header.Get("ETag") != `"upstream-tag"` {
					t.Fatalf("passthrough response changed: %v %s", response.Header, body)
				}
			}
		})
	}
}

func TestModelsResponseGzipAndValidation(t *testing.T) {
	for _, test := range []struct {
		name     string
		body     string
		encoding string
		status   int
	}{
		{"gzip", `{"models":[]}`, "gzip", http.StatusOK},
		{"invalid-json", `{`, "identity", http.StatusBadGateway},
		{"missing-models", `{}`, "identity", http.StatusBadGateway},
		{"unsupported-encoding", `{"models":[]}`, "br", http.StatusBadGateway},
		{"oversized", strings.Repeat(" ", maxModelsResponseBytes+1), "gzip", http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			var body bytes.Buffer
			if test.encoding == "gzip" {
				writer := gzip.NewWriter(&body)
				if _, err := io.WriteString(writer, test.body); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				body.WriteString(test.body)
			}
			response := &http.Response{Header: make(http.Header), Body: io.NopCloser(&body)}
			defer response.Body.Close()
			response.Header.Set("Content-Encoding", test.encoding)
			response.Header.Set("Content-Length", strconv.Itoa(body.Len()))
			response.Header.Set("ETag", `W/"native"`)
			response.Header.Set("Content-MD5", "native")
			response.Header.Set("Digest", "native")
			service := &Service{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			recorder := httptest.NewRecorder()
			service.writeModelsResponse(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil), response)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if test.status == http.StatusOK {
				for _, header := range []string{"Content-Length", "Content-Encoding", "ETag", "Content-MD5", "Digest"} {
					if recorder.Header().Get(header) != "" {
						t.Errorf("transformed response retains %s", header)
					}
				}
				assertCatalogJSONEqual(t, recorder.Body.Bytes(), []byte(`{"models":[],"object":"list","data":[]}`))
			}
		})
	}
}
