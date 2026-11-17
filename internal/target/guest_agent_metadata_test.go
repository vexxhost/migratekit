package target

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

func TestReusedVolumeGuestAgentMetadata(t *testing.T) {
	for _, tt := range []struct {
		name     string
		enabled  bool
		current  string
		getError bool
		setError bool
		gets     int
		sets     int
	}{
		{name: "not requested"},
		{name: "opt out preserves existing setting", current: "yes"},
		{name: "enable on reused volume", enabled: true, gets: 1, sets: 1},
		{name: "enable previously disabled volume", enabled: true, current: "no", gets: 1, sets: 1},
		{name: "already enabled", enabled: true, current: "yes", gets: 1},
		{name: "read error", enabled: true, getError: true, gets: 1},
		{name: "update error", enabled: true, setError: true, gets: 1, sets: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			gets, sets := 0, 0
			metadata := map[string]string{"hw_disk_bus": "scsi", "os_type": "windows"}
			if tt.current != "" {
				metadata["hw_qemu_guest_agent"] = tt.current
			}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/volumes/disk":
					gets++
					if tt.getError {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{"id": "disk", "volume_image_metadata": metadata}})
				case r.Method == http.MethodPost && r.URL.Path == "/volumes/disk/action":
					sets++
					var body map[string]struct {
						Metadata map[string]string `json:"metadata"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					update := body["os-set_image_metadata"].Metadata
					if len(update) != 1 || update["hw_qemu_guest_agent"] != "yes" {
						t.Errorf("unexpected image property update: %v", update)
					}
					if tt.setError {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					for key, value := range update {
						metadata[key] = value
					}
					json.NewEncoder(w).Encode(map[string]any{"metadata": metadata})
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer api.Close()
			destination := &OpenStack{ClientSet: &openstack.ClientSet{BlockStorage: &gophercloud.ServiceClient{
				ProviderClient: &gophercloud.ProviderClient{HTTPClient: *api.Client()}, Endpoint: api.URL + "/",
			}}}
			ctx := context.WithValue(context.Background(), "enableQemuGuestAgent", tt.enabled)
			err := destination.ensureGuestAgentMetadata(ctx, "disk")
			if (err != nil) != (tt.getError || tt.setError) {
				t.Fatalf("unexpected error: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if gets != tt.gets || sets != tt.sets {
				t.Fatalf("API calls get=%d set=%d; want get=%d set=%d", gets, sets, tt.gets, tt.sets)
			}
			if metadata["hw_disk_bus"] != "scsi" || metadata["os_type"] != "windows" {
				t.Fatal("unrelated image properties changed")
			}
			if tt.enabled && !tt.getError && !tt.setError && metadata["hw_qemu_guest_agent"] != "yes" {
				t.Fatal("guest agent was not enabled on the reused volume")
			}
		})
	}
}

func TestConnectUpdatesReusedGuestAgentBeforeDeviceLookup(t *testing.T) {
	var mu sync.Mutex
	lists, updates := 0, 0
	metadata := map[string]string{"hw_disk_bus": "virtio"}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/volumes/detail":
			lists++
			if lists > 1 {
				// Stop before device lookup or attachment: metadata must be set
				// before Connect reaches this second lookup.
				w.WriteHeader(http.StatusTeapot)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"volumes": []any{map[string]string{"id": "disk"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/volumes/disk":
			json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{"id": "disk", "volume_image_metadata": metadata}})
		case r.Method == http.MethodPost && r.URL.Path == "/volumes/disk/action":
			var body map[string]struct {
				Metadata map[string]string `json:"metadata"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for key, value := range body["os-set_image_metadata"].Metadata {
				metadata[key] = value
			}
			if metadata["hw_qemu_guest_agent"] == "yes" {
				updates++
			}
			json.NewEncoder(w).Encode(map[string]any{"metadata": metadata})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()
	destination := &OpenStack{
		VirtualMachine: object.NewVirtualMachine(nil, types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-1"}),
		Disk:           &types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2000}},
		ClientSet: &openstack.ClientSet{BlockStorage: &gophercloud.ServiceClient{
			ProviderClient: &gophercloud.ProviderClient{HTTPClient: *api.Client()}, Endpoint: api.URL + "/",
		}},
	}
	ctx := context.WithValue(context.Background(), "enableQemuGuestAgent", true)
	ctx = context.WithValue(ctx, "volumeCreateOpts", &VolumeCreateOpts{BusType: "virtio"})
	ctx = context.WithValue(ctx, "vzUnsafeVolumeByName", false)
	ctx = context.WithValue(ctx, "osType", "")
	if err := destination.Connect(ctx); err == nil {
		t.Fatal("test should stop before device lookup")
	}
	mu.Lock()
	defer mu.Unlock()
	if updates != 1 || lists != 2 {
		t.Fatalf("reused volume guest agent update count=%d, volume lookups=%d; want 1 update before the second lookup", updates, lists)
	}
}
