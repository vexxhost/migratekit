package openstack

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCloudRequestTimeout(t *testing.T) {
	for _, partialBody := range []bool{false, true} {
		name := "headers"
		if partialBody {
			name = "response-body"
		}
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if partialBody {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusMultipleChoices)
					_, _ = w.Write([]byte(`{"versions":`))
					w.(http.Flusher).Flush()
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			t.Setenv("OS_AUTH_URL", server.URL)
			t.Setenv("OS_USERNAME", "test-user")
			t.Setenv("OS_PASSWORD", "test-password")
			t.Setenv("OS_PROJECT_ID", "test-project")
			t.Setenv("OS_USER_DOMAIN_NAME", "Default")
			t.Setenv("OS_TOKEN", "")
			result := make(chan error, 1)
			go func() {
				_, err := newClientSet(context.Background(), 50*time.Millisecond)
				result <- err
			}()
			select {
			case err := <-result:
				var timeout net.Error
				if !errors.As(err, &timeout) || !timeout.Timeout() {
					t.Fatalf("expected a request timeout, got %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cloud request did not return after its timeout")
			}
		})
	}
}
