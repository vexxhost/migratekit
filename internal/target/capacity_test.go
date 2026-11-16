package target

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

func TestConnectRejectsSmallerExistingVolumeBeforeMutation(t *testing.T) {
	const id = "12345678-1234-1234-1234-123456789abc"
	requests := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/volumes/detail":
			json.NewEncoder(w).Encode(map[string]any{"volumes": []any{map[string]string{"id": id}}})
		case r.Method == http.MethodGet && r.URL.Path == "/volumes/"+id:
			json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{"id": id, "size": 2, "metadata": map[string]string{"change_id": "epoch/1"}}})
		default:
			t.Errorf("unexpected request before capacity rejection: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected mutation", http.StatusBadRequest)
		}
	}))
	defer api.Close()
	vm := object.NewVirtualMachine(nil, types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-test"})
	destination := &OpenStack{VirtualMachine: vm, Disk: &types.VirtualDisk{CapacityInBytes: 3 * 1024 * 1024 * 1024}, ClientSet: &openstack.ClientSet{BlockStorage: &gophercloud.ServiceClient{ProviderClient: &gophercloud.ProviderClient{HTTPClient: *api.Client()}, Endpoint: api.URL + "/"}}}
	ctx := context.WithValue(context.Background(), "vzUnsafeVolumeByName", false)
	ctx = context.WithValue(ctx, "volumeCreateOpts", &VolumeCreateOpts{})
	err := destination.Connect(ctx)
	if err == nil || !strings.Contains(err.Error(), "requires 3 GiB") || !strings.Contains(err.Error(), "change_id") || requests != 2 {
		t.Fatalf("missing pre-attachment capacity rejection: requests=%d error=%v", requests, err)
	}
}
