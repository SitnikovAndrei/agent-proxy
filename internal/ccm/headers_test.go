package ccm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ocm/internal/config"
)

func TestOAuthHeadersAndClientCredentials(t *testing.T) {
	cases := []struct{ name, clientBeta, configuredBeta, expected string }{
		{name: "absent", expected: anthropicBetaOAuthValue},
		{name: "client flags", clientBeta: "feature-a,feature-b", expected: anthropicBetaOAuthValue + ",feature-a,feature-b"},
		{name: "existing oauth", clientBeta: "feature-a, " + anthropicBetaOAuthValue, expected: "feature-a, " + anthropicBetaOAuthValue},
		{name: "configured flags", clientBeta: "client-feature", configuredBeta: "configured-feature", expected: anthropicBetaOAuthValue + ",configured-feature"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic-access" {
					t.Error("provider authorization missing or client authorization forwarded")
				}
				if r.Header.Get("X-Api-Key") != "" {
					t.Error("client API key forwarded")
				}
				beta := r.Header.Get("Anthropic-Beta")
				if beta != test.expected || strings.Count(beta, anthropicBetaOAuthValue) != 1 {
					t.Errorf("beta=%q", beta)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer upstream.Close()
			service := requestTestService(t, upstream.URL)
			service.users.UpdateUsers([]config.User{{Name: "alice", Token: "client-secret"}})
			if test.configuredBeta != "" {
				service.headers = http.Header{"Anthropic-Beta": {test.configuredBeta}, "Authorization": {"Bearer configured-secret"}, "X-Api-Key": {"configured-key"}}
			}
			request := httptest.NewRequest("GET", "/v1/models", nil)
			request.Header.Set("Authorization", "Bearer client-secret")
			request.Header.Set("X-Api-Key", "client-api-key")
			if test.clientBeta != "" {
				request.Header.Set("Anthropic-Beta", test.clientBeta)
			}
			writer := httptest.NewRecorder()
			service.ServeHTTP(writer, request)
			if writer.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
			}
		})
	}
}
