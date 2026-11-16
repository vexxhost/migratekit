package target

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vexxhost/migratekit/internal/vmware"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

type guestProperties struct{ reference types.ManagedObjectReference }

func (f guestProperties) RoundTrip(_ context.Context, req, res soap.HasFault) error {
	body, ok := res.(*methods.RetrievePropertiesExBody)
	if !ok {
		return fmt.Errorf("unexpected SOAP request %T", req)
	}
	body.Res = &types.RetrievePropertiesExResponse{Returnval: &types.RetrieveResult{Objects: []types.ObjectContent{{Obj: f.reference, PropSet: []types.DynamicProperty{{Name: "config", Val: &types.VirtualMachineConfigInfo{GuestId: "winLonghorn64Guest", BootOptions: &types.VirtualMachineBootOptions{}}}}}}}}
	return nil
}
func TestExistingVolumeReceivesRequestedOSType(t *testing.T) {
	for _, option := range []string{"auto", "windows", ""} {
		t.Run(option, func(t *testing.T) {
			reference := types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-test"}
			client := &vim25.Client{ServiceContent: types.ServiceContent{PropertyCollector: types.ManagedObjectReference{Type: "PropertyCollector", Value: "collector"}}, RoundTripper: guestProperties{reference}}
			vm := object.NewVirtualMachine(client, reference)
			vm.InventoryPath = "/dc/vm/test-guest"
			var guest mo.VirtualMachine
			if err := vm.Properties(context.Background(), reference, []string{"config.guestId"}, &guest); err != nil {
				t.Fatal(err)
			}
			if guest.Config == nil || !vmware.IsWindowsGuest(guest.Config.GuestId) {
				t.Fatal("fixture must be a recognized legacy Windows VM")
			}
			const id = "12345678-1234-1234-1234-123456789abc"
			var lists atomic.Int32
			var corrected atomic.Bool
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "GET" && r.URL.Path == "/volumes/detail":
					if lists.Add(1) > 1 {
						http.Error(w, "stop before attachment", 503)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"volumes": []any{map[string]string{"id": id}}})
				case r.Method == "GET" && r.URL.Path == "/volumes/"+id:
					json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{"id": id, "metadata": map[string]string{}, "volume_image_metadata": map[string]string{"os_type": "linux", "hw_disk_bus": "virtio"}}})
				case r.Method == "POST" && r.URL.Path == "/volumes/"+id+"/action":
					var body map[string]struct {
						Metadata map[string]string `json:"metadata"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["os-set_image_metadata"].Metadata["os_type"] == "windows" {
						corrected.Store(true)
					}
					json.NewEncoder(w).Encode(map[string]any{"metadata": body["os-set_image_metadata"].Metadata})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer api.Close()
			destination := &OpenStack{VirtualMachine: vm, Disk: &types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2000}}, ClientSet: &openstack.ClientSet{BlockStorage: &gophercloud.ServiceClient{ProviderClient: &gophercloud.ProviderClient{HTTPClient: *api.Client()}, Endpoint: api.URL + "/"}}}
			ctx := context.WithValue(context.Background(), "vzUnsafeVolumeByName", false)
			ctx = context.WithValue(ctx, "volumeCreateOpts", &VolumeCreateOpts{BusType: "virtio"})
			ctx = context.WithValue(ctx, "osType", option)
			ctx = context.WithValue(ctx, "enableQemuGuestAgent", false)
			if err := destination.Connect(ctx); err == nil {
				t.Fatal("test should stop before actual attachment")
			}
			if corrected.Load() != (option != "") {
				t.Fatalf("existing Windows volume keeps os_type=linux with --os-type=%s", option)
			}
		})
	}
}
