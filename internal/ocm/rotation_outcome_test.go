package ocm

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ocm/internal/persistence"
)

func TestRefreshRotationSendOutcome(t *testing.T) {
	for _, sent := range []bool{false, true} {
		name := "before send"
		if sent {
			name = "after send"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			endpoint := "http://" + listener.Addr().String()
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			if sent {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					w.WriteHeader(http.StatusInternalServerError)
				}))
				defer server.Close()
				endpoint = server.URL
			}
			store := testCredentialStore(t, endpoint, time.Second)
			if _, err := store.load(context.Background()); err == nil {
				t.Fatal("expected initial refresh failure")
			}
			_, err = store.load(context.Background())
			if err == nil {
				t.Fatal("expected retry failure")
			}
			if errors.Is(err, persistence.ErrUncertainRotation) != sent {
				t.Fatalf("sent=%v retry error: %v", sent, err)
			}
			if !sent {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
				}))
				defer server.Close()
				store.tokenURL = server.URL
				if _, err := store.load(context.Background()); err != nil {
					t.Fatalf("same tokens should be retryable: %v", err)
				}
			}
		})
	}
}
