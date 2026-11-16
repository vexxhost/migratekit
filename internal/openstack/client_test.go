package openstack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
)

func TestNewClientSetEndpointInterface(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		unset     bool
		want      string
		wantError bool
	}{
		{name: "unset", unset: true, want: "public"},
		{name: "empty", want: "public"},
		{name: "public", value: "public", want: "public"},
		{name: "internal", value: "internal", want: "internal"},
		{name: "admin", value: "admin", want: "admin"},
		{name: "invalid", value: "private", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{
				"OS_USERNAME", "OS_USERID", "OS_PASSWORD", "OS_PASSCODE",
				"OS_PROJECT_ID", "OS_PROJECT_NAME", "OS_TENANT_ID", "OS_TENANT_NAME",
				"OS_DOMAIN_ID", "OS_DOMAIN_NAME", "OS_SYSTEM_SCOPE",
				"OS_APPLICATION_CREDENTIAL_ID", "OS_APPLICATION_CREDENTIAL_NAME",
				"OS_APPLICATION_CREDENTIAL_SECRET", "OS_INSECURE",
			} {
				t.Setenv(name, "")
			}
			t.Setenv("OS_USERID", "test-user")
			t.Setenv("OS_PASSWORD", "test-password")
			t.Setenv("OS_PROJECT_ID", "test-project")
			t.Setenv("OS_REGION_NAME", "test-region")
			t.Setenv("OS_INTERFACE", tt.value)
			if tt.unset {
				if err := os.Unsetenv("OS_INTERFACE"); err != nil {
					t.Fatal(err)
				}
			}

			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/v3/auth/tokens" {
					http.NotFound(w, r)
					return
				}
				var catalog []map[string]any
				for _, service := range []struct{ kind, path string }{
					{"block-storage", "volume/v3"},
					{"compute", "compute/v2.1"},
					{"network", "network/v2.0"},
				} {
					var endpoints []map[string]string
					for _, region := range []string{"other-region", "test-region"} {
						for _, iface := range []string{"public", "internal", "admin"} {
							endpoints = append(endpoints, map[string]string{
								"region":    region,
								"interface": iface,
								"url":       "http://" + r.Host + "/" + region + "/" + iface + "/" + service.path + "/",
							})
						}
					}
					catalog = append(catalog, map[string]any{"type": service.kind, "endpoints": endpoints})
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Subject-Token", "test-token")
				w.WriteHeader(http.StatusCreated)
				if err := json.NewEncoder(w).Encode(map[string]any{
					"token": map[string]any{"catalog": catalog},
				}); err != nil {
					t.Errorf("encode token response: %v", err)
				}
			}))
			t.Cleanup(server.Close)
			t.Setenv("OS_AUTH_URL", server.URL+"/v3")

			clients, err := NewClientSet(context.Background())
			if tt.wantError {
				if err == nil {
					t.Fatal("expected an OS_INTERFACE error")
				}
				for _, part := range []string{"OS_INTERFACE", "private", "public", "internal", "admin"} {
					if !strings.Contains(err.Error(), part) {
						t.Errorf("interface error %q does not mention %q", err, part)
					}
				}
				if requests.Load() != 0 {
					t.Fatal("invalid interface contacted Keystone before returning an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, service := range []struct {
				name   string
				client *gophercloud.ServiceClient
				path   string
			}{
				{"Cinder", clients.BlockStorage, "volume/v3/"},
				{"Nova", clients.Compute, "compute/v2.1/"},
				{"Neutron", clients.Networking, "network/v2.0/"},
			} {
				want := server.URL + "/test-region/" + tt.want + "/" + service.path
				if service.client.Endpoint != want {
					t.Errorf("%s endpoint = %q, want %q", service.name, service.client.Endpoint, want)
				}
			}
		})
	}
}
