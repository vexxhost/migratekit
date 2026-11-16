package target

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vexxhost/migratekit/internal/vmware"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

func checkpointTarget(api *httptest.Server) *OpenStack {
	vm := object.NewVirtualMachine(nil, types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-test"})
	vm.InventoryPath = "/dc/vm/test-guest"
	return &OpenStack{VirtualMachine: vm, Disk: &types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2000}}, ClientSet: &openstack.ClientSet{BlockStorage: &gophercloud.ServiceClient{ProviderClient: &gophercloud.ProviderClient{HTTPClient: *api.Client()}, Endpoint: api.URL + "/"}}}
}
func TestCheckpointReturnsLookupFailure(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "temporarily unavailable"}})
	}))
	defer api.Close()
	ctx := context.WithValue(context.Background(), "vzUnsafeVolumeByName", false)
	err := checkpointTarget(api).WriteChangeID(ctx, &vmware.ChangeID{})
	var response gophercloud.ErrUnexpectedResponseCode
	if !errors.As(err, &response) || response.Actual != http.StatusServiceUnavailable {
		t.Fatalf("lookup error was not preserved: %v", err)
	}
}
func TestCheckpointInitializesMissingMetadata(t *testing.T) {
	const id = "12345678-1234-1234-1234-123456789abc"
	var updated bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/volumes/detail":
			json.NewEncoder(w).Encode(map[string]any{"volumes": []any{map[string]string{"id": id}}})
		case r.Method == "GET" && r.URL.Path == "/volumes/"+id:
			json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{"id": id, "metadata": nil}})
		case r.Method == "PUT" && r.URL.Path == "/volumes/"+id:
			var body struct {
				Volume struct {
					Metadata map[string]string `json:"metadata"`
				} `json:"volume"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Volume.Metadata["change_id"] != "epoch/1" {
				t.Errorf("unexpected metadata: %v", body.Volume.Metadata)
			}
			updated = true
			json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{"id": id, "metadata": body.Volume.Metadata}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer api.Close()
	ctx := context.WithValue(context.Background(), "vzUnsafeVolumeByName", false)
	if err := checkpointTarget(api).WriteChangeID(ctx, &vmware.ChangeID{Value: "epoch/1"}); err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("checkpoint was not written")
	}
}
