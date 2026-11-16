package target

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

func TestConnectUpdatesBusOnPreviouslyReplicatedVolume(t *testing.T) {
	for _, tc := range []struct {
		name, bus      string
		existing, want map[string]string
	}{
		{"legacy ordinary metadata", "scsi", nil, map[string]string{"hw_disk_bus": "scsi", "hw_scsi_model": "virtio-scsi"}},
		{"virtio to scsi", "scsi", map[string]string{"hw_disk_bus": "virtio"}, map[string]string{"hw_disk_bus": "scsi", "hw_scsi_model": "virtio-scsi"}},
		{"scsi to virtio", "virtio", map[string]string{"hw_disk_bus": "scsi", "hw_scsi_model": "virtio-scsi"}, map[string]string{"hw_disk_bus": "virtio"}},
		{"already configured", "scsi", map[string]string{"hw_disk_bus": "scsi", "hw_scsi_model": "virtio-scsi"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "12345678-1234-1234-1234-123456789abc"
			var got map[string]string
			lists := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/volumes/detail":
					lists++
					if lists > 1 {
						http.Error(w, "stop before local attachment", http.StatusServiceUnavailable)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"volumes": []any{map[string]string{"id": id}}})
				case r.Method == http.MethodGet && r.URL.Path == "/volumes/"+id:
					json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{"id": id, "metadata": map[string]string{"hw_disk_bus": "scsi"}, "volume_image_metadata": tc.existing}})
				case r.Method == http.MethodPost && r.URL.Path == "/volumes/"+id+"/action":
					var body struct {
						Action struct {
							Metadata map[string]string `json:"metadata"`
						} `json:"os-set_image_metadata"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					got = body.Action.Metadata
					json.NewEncoder(w).Encode(map[string]any{"metadata": got})
				default:
					t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusInternalServerError)
				}
			}))
			defer api.Close()
			vm := object.NewVirtualMachine(nil, types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-test"})
			vm.InventoryPath = "/dc/vm/test-guest"
			destination := &OpenStack{
				VirtualMachine: vm,
				Disk:           &types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2000}},
				ClientSet:      &openstack.ClientSet{BlockStorage: &gophercloud.ServiceClient{ProviderClient: &gophercloud.ProviderClient{HTTPClient: *api.Client()}, Endpoint: api.URL + "/"}},
			}
			ctx := context.WithValue(context.Background(), "vzUnsafeVolumeByName", false)
			ctx = context.WithValue(ctx, "volumeCreateOpts", &VolumeCreateOpts{BusType: tc.bus})
			if err := destination.Connect(ctx); err == nil {
				t.Fatal("test should stop before local attachment")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("bus image metadata=%v; want %v", got, tc.want)
			}
		})
	}
}
