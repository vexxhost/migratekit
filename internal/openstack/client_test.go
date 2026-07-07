package openstack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
)

func TestEnableReauthenticationAllowsReusableAuth(t *testing.T) {
	opts := gophercloud.AuthOptions{}

	enableReauthentication(&opts)

	if !opts.AllowReauth {
		t.Fatal("expected reusable auth options to allow reauthentication")
	}
}

func TestEnableReauthenticationPreservesTokenOnlyAuth(t *testing.T) {
	opts := gophercloud.AuthOptions{
		TokenID: "existing-token",
	}

	enableReauthentication(&opts)

	if opts.AllowReauth {
		t.Fatal("expected token-only auth options to keep reauthentication disabled")
	}
}

func TestReauthRetriesUnauthorizedRequestOnce(t *testing.T) {
	var requests int
	var reauths int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		if r.URL.Path != "/volumes/detail" {
			http.Error(w, fmt.Sprintf("unexpected path %q", r.URL.Path), http.StatusNotFound)
			return
		}

		switch requests {
		case 1:
			if got := r.Header.Get("X-Auth-Token"); got != "stale-token" {
				http.Error(w, fmt.Sprintf("expected stale token, got %q", got), http.StatusBadRequest)
				return
			}
			http.Error(w, `{"error":{"code":401,"title":"Unauthorized"}}`, http.StatusUnauthorized)
		case 2:
			if got := r.Header.Get("X-Auth-Token"); got != "fresh-token" {
				http.Error(w, fmt.Sprintf("expected fresh token, got %q", got), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"volumes":[]}`)
		default:
			http.Error(w, "unexpected retry", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	provider, blockStorage := newTestBlockStorageClient(server.URL, "stale-token")
	provider.ReauthFunc = func(ctx context.Context) error {
		reauths++
		if got := reauthOperationFromContext(ctx); got != "test volume list" {
			t.Fatalf("expected reauth operation %q, got %q", "test volume list", got)
		}
		provider.SetToken("fresh-token")
		return nil
	}
	wrapReauthLogging(provider)

	ctx := WithReauthOperation(context.Background(), "test volume list")
	pages, err := volumes.List(blockStorage, nil).AllPages(ctx)
	if err != nil {
		t.Fatalf("expected volume list to succeed after reauth, got %v", err)
	}

	volumeList, err := volumes.ExtractVolumes(pages)
	if err != nil {
		t.Fatalf("expected volume extraction to succeed, got %v", err)
	}
	if len(volumeList) != 0 {
		t.Fatalf("expected no volumes, got %d", len(volumeList))
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
	if reauths != 1 {
		t.Fatalf("expected 1 reauthentication, got %d", reauths)
	}
}

func TestReauthDoesNotRetryUnauthorizedEndlessly(t *testing.T) {
	var requests int
	var reauths int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"error":{"code":401,"title":"Unauthorized"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	provider, blockStorage := newTestBlockStorageClient(server.URL, "stale-token")
	provider.ReauthFunc = func(context.Context) error {
		reauths++
		provider.SetToken("fresh-token")
		return nil
	}
	wrapReauthLogging(provider)

	_, err := volumes.List(blockStorage, nil).AllPages(
		WithReauthOperation(context.Background(), "test repeated 401"),
	)
	if err == nil {
		t.Fatal("expected repeated 401 to fail")
	}

	var afterReauth *gophercloud.ErrErrorAfterReauthentication
	if !errors.As(err, &afterReauth) {
		t.Fatalf("expected ErrErrorAfterReauthentication, got %T: %v", err, err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
	if reauths != 1 {
		t.Fatalf("expected 1 reauthentication, got %d", reauths)
	}
}

func newTestBlockStorageClient(endpoint string, token string) (*gophercloud.ProviderClient, *gophercloud.ServiceClient) {
	provider := &gophercloud.ProviderClient{}
	provider.UseTokenLock()
	provider.SetToken(token)

	return provider, &gophercloud.ServiceClient{
		ProviderClient: provider,
		Endpoint:       endpoint + "/",
		ResourceBase:   endpoint + "/",
		Type:           "volumev3",
	}
}
