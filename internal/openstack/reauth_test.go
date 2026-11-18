package openstack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
)

func TestClientSetRenewsRejectedTokens(t *testing.T) {
	for _, mode := range []string{"password", "application credential"} {
		t.Run(mode, func(t *testing.T) {
			for _, key := range []string{"OS_AUTH_URL", "OS_USERNAME", "OS_USERID", "OS_PASSWORD", "OS_PASSCODE", "OS_PROJECT_ID", "OS_PROJECT_NAME", "OS_TENANT_ID", "OS_TENANT_NAME", "OS_DOMAIN_ID", "OS_DOMAIN_NAME", "OS_APPLICATION_CREDENTIAL_ID", "OS_APPLICATION_CREDENTIAL_NAME", "OS_APPLICATION_CREDENTIAL_SECRET", "OS_SYSTEM_SCOPE", "OS_REGION_NAME"} {
				t.Setenv(key, "")
			}
			t.Setenv("OS_REGION_NAME", "test-region")
			if mode == "password" {
				t.Setenv("OS_USERNAME", "test-user")
				t.Setenv("OS_PASSWORD", "test-password")
				t.Setenv("OS_DOMAIN_ID", "default")
				t.Setenv("OS_PROJECT_ID", "test-project")
			} else {
				t.Setenv("OS_APPLICATION_CREDENTIAL_ID", "test-application")
				t.Setenv("OS_APPLICATION_CREDENTIAL_SECRET", "test-secret")
			}
			var authCalls, volumeCalls atomic.Int32
			var api *httptest.Server
			api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "POST" && r.URL.Path == "/v3/auth/tokens":
					var request struct {
						Auth struct {
							Identity struct {
								Methods []string `json:"methods"`
							} `json:"identity"`
						} `json:"auth"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					want := "password"
					if mode == "application credential" {
						want = "application_credential"
					}
					if strings.Join(request.Auth.Identity.Methods, ",") != want {
						t.Errorf("authentication methods = %v", request.Auth.Identity.Methods)
					}
					count := authCalls.Add(1)
					w.Header().Set("X-Subject-Token", fmt.Sprintf("token-%d", count))
					catalog := []any{}
					for _, service := range []struct{ kind, path string }{{"volumev3", "cinder/v3/test-project"}, {"compute", "nova/v2.1"}, {"network", "neutron/v2.0"}} {
						catalog = append(catalog, map[string]any{"type": service.kind, "name": service.path, "endpoints": []any{map[string]string{"interface": "public", "region": "test-region", "url": api.URL + "/" + service.path + "/"}}})
					}
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(map[string]any{"token": map[string]any{"expires_at": "2999-01-01T00:00:00Z", "catalog": catalog}})
				case r.Method == "GET" && r.URL.Path == "/cinder/v3/test-project/volumes/detail":
					volumeCalls.Add(1)
					if r.Header.Get("X-Auth-Token") == "token-1" {
						w.WriteHeader(http.StatusUnauthorized)
						fmt.Fprint(w, `{"error":{"message":"token expired"}}`)
						return
					}
					if r.Header.Get("X-Auth-Token") != "token-2" {
						t.Error("unexpected replacement token")
					}
					fmt.Fprint(w, `{"volumes":[]}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer api.Close()
			t.Setenv("OS_AUTH_URL", api.URL+"/v3")
			clients, err := NewClientSet(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := volumes.List(clients.BlockStorage, volumes.ListOpts{}).AllPages(context.Background()); err != nil {
				t.Fatal(err)
			}
			if authCalls.Load() != 2 || volumeCalls.Load() != 2 {
				t.Fatalf("auth calls %d; volume calls %d", authCalls.Load(), volumeCalls.Load())
			}
			if clients.Compute.Token() != "token-2" || clients.Networking.Token() != "token-2" {
				t.Fatal("renewed token not shared across service clients")
			}
		})
	}
}
