package target

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

type metadataTransport func(*http.Request) (*http.Response, error)

func (f metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDisconnectUsesAttachmentRecords(t *testing.T) {
	for _, scenario := range []string{"owned", "foreign", "pending", "available", "bad-metadata"} {
		t.Run(scenario, func(t *testing.T) {
			const volumeID = "12345678-1234-1234-1234-123456789abc"
			const runnerID = "abcdefab-1234-1234-1234-123456789abc"
			var deleted atomic.Int32
			original := http.DefaultTransport
			http.DefaultTransport = metadataTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "169.254.169.254" {
					return original.RoundTrip(r)
				}
				status := 200
				body := `{"uuid":"` + runnerID + `"}`
				if scenario == "bad-metadata" {
					status = 503
					body = `{"error":"unavailable"}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = original })
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "GET" && r.URL.Path == "/volumes/detail":
					json.NewEncoder(w).Encode(map[string]any{"volumes": []any{map[string]string{"id": volumeID}}})
				case r.Method == "GET" && r.URL.Path == "/volumes/"+volumeID:
					status := "in-use"
					var attachments []any
					switch scenario {
					case "owned", "bad-metadata":
						attachments = []any{map[string]string{"server_id": runnerID}}
					case "foreign":
						attachments = []any{map[string]string{"server_id": "other-instance"}}
					case "pending":
						status = "attaching"
					case "available":
						status = "available"
					}
					if deleted.Load() > 0 {
						status = "available"
						attachments = nil
					}
					json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{"id": volumeID, "status": status, "attachments": attachments}})
				case r.Method == "GET" && r.URL.Path == "/servers/"+runnerID+"/os-volume_attachments":
					if scenario != "pending" {
						t.Errorf("unexpected Nova lookup for %s", scenario)
					}
					json.NewEncoder(w).Encode(map[string]any{"volumeAttachments": []any{map[string]string{"volumeId": volumeID, "serverId": runnerID, "id": volumeID}}})
				case r.Method == "DELETE" && r.URL.Path == "/servers/"+runnerID+"/os-volume_attachments/"+volumeID:
					deleted.Add(1)
					w.WriteHeader(http.StatusAccepted)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer api.Close()
			vm := object.NewVirtualMachine(nil, types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-test"})
			vm.InventoryPath = "/dc/vm/test-guest"
			service := &gophercloud.ServiceClient{ProviderClient: &gophercloud.ProviderClient{HTTPClient: *api.Client()}, Endpoint: api.URL + "/"}
			destination := &OpenStack{VirtualMachine: vm, Disk: &types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2000}}, ClientSet: &openstack.ClientSet{BlockStorage: service, Compute: service}, attachmentRequested: scenario == "pending"}
			ctx := context.WithValue(context.Background(), "vzUnsafeVolumeByName", false)
			err := destination.Disconnect(ctx)
			if (err != nil) != (scenario == "bad-metadata") {
				t.Fatalf("unexpected disconnect result: %v", err)
			}
			want := int32(0)
			if scenario == "owned" || scenario == "pending" {
				want = 1
			}
			if deleted.Load() != want {
				t.Fatalf("detach calls=%d want=%d", deleted.Load(), want)
			}
		})
	}
}
