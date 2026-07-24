package target

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	log "github.com/sirupsen/logrus"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

const (
	testVolumeID = "cb562d94-3cd8-42b2-abf6-7e846a639df8"
	testServerID = "helper-server"
)

type deviceResult struct {
	path string
	err  error
}

type testVolumeState struct {
	status      string
	attachments []volumes.Attachment
	statusCode  int
}

func TestVolumeDetachComplete(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		attachmentCount int
		devicePath      string
		want            bool
	}{
		{
			name:   "available without attachments or device",
			status: "available",
			want:   true,
		},
		{
			name:            "available with attachment",
			status:          "available",
			attachmentCount: 1,
			want:            false,
		},
		{
			name:       "available with local device",
			status:     "available",
			devicePath: "/dev/sdb",
			want:       false,
		},
		{
			name:   "still detaching",
			status: "detaching",
			want:   false,
		},
		{
			name:   "case insensitive available",
			status: "AVAILABLE",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := volumeDetachComplete(tt.status, tt.attachmentCount, tt.devicePath)
			if got != tt.want {
				t.Fatalf("volumeDetachComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindDeviceInDir(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, deviceDir string) string
		wantDevice bool
	}{
		{
			name: "device exists and remains attached",
			setup: func(t *testing.T, deviceDir string) string {
				devicePath := filepath.Join(t.TempDir(), "vde")
				if err := os.WriteFile(devicePath, []byte{}, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(devicePath, filepath.Join(deviceDir, "virtio-"+testVolumeID[:18])); err != nil {
					t.Fatal(err)
				}
				return devicePath
			},
			wantDevice: true,
		},
		{
			name: "missing symlink target is treated as absent device",
			setup: func(t *testing.T, deviceDir string) string {
				devicePath := filepath.Join(t.TempDir(), "vde")
				if err := os.Symlink(devicePath, filepath.Join(deviceDir, "virtio-"+testVolumeID[:18])); err != nil {
					t.Fatal(err)
				}
				return devicePath
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceDir := t.TempDir()
			devicePath := tt.setup(t, deviceDir)

			got, err := findDeviceInDir(testVolumeID, deviceDir)
			if err != nil {
				t.Fatalf("findDeviceInDir() returned error: %v", err)
			}
			if tt.wantDevice && got != devicePath {
				t.Fatalf("findDeviceInDir() = %q, want %q", got, devicePath)
			}
			if !tt.wantDevice && got != "" {
				t.Fatalf("findDeviceInDir() = %q, want empty path", got)
			}
		})
	}
}

func TestFindDeviceInDirReturnsUnexpectedFilesystemErrors(t *testing.T) {
	deviceDir := t.TempDir()
	firstLink := filepath.Join(deviceDir, "virtio-"+testVolumeID[:18])
	secondLink := filepath.Join(deviceDir, "loop")

	if err := os.Symlink(secondLink, firstLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(firstLink, secondLink); err != nil {
		t.Fatal(err)
	}

	_, err := findDeviceInDir(testVolumeID, deviceDir)
	if err == nil {
		t.Fatal("expected symlink loop to return an error")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected non-missing filesystem error, got %v", err)
	}
}

func TestWaitForVolumeDetachedHandlesAsyncStateOrdering(t *testing.T) {
	tests := []struct {
		name         string
		states       []testVolumeState
		devices      []deviceResult
		wantRequests int
	}{
		{
			name: "local device disappears immediately after detach",
			states: []testVolumeState{
				{status: "available"},
			},
			devices:      []deviceResult{{path: ""}},
			wantRequests: 1,
		},
		{
			name: "local device disappears before OpenStack attachment clears",
			states: []testVolumeState{
				{
					status: "in-use",
					attachments: []volumes.Attachment{
						testAttachment(),
					},
				},
				{status: "available"},
			},
			devices:      []deviceResult{{path: ""}, {path: ""}},
			wantRequests: 2,
		},
		{
			name: "OpenStack attachment clears before local device disappears",
			states: []testVolumeState{
				{status: "available"},
				{status: "available"},
			},
			devices:      []deviceResult{{path: "/dev/vde"}, {path: ""}},
			wantRequests: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withDetachPolling(t, 100*time.Millisecond, time.Millisecond)
			setFindVolumeDeviceResults(t, tt.devices)

			blockStorage, requestCount := newTestBlockStorageClient(t, testVolumeID, tt.states)
			target := &OpenStack{
				ClientSet: &openstack.ClientSet{
					BlockStorage: blockStorage,
				},
			}

			err := target.waitForVolumeDetached(context.Background(), testVolumeID, testServerID, testLogger())
			if err != nil {
				t.Fatalf("waitForVolumeDetached() returned error: %v", err)
			}
			if *requestCount != tt.wantRequests {
				t.Fatalf("expected %d OpenStack requests, got %d", tt.wantRequests, *requestCount)
			}
		})
	}
}

func TestWaitForVolumeDetachedTimesOutWhenHelperAttachmentRemains(t *testing.T) {
	withDetachPolling(t, 10*time.Millisecond, time.Millisecond)
	setFindVolumeDeviceResults(t, []deviceResult{{path: ""}})

	blockStorage, _ := newTestBlockStorageClient(t, testVolumeID, []testVolumeState{
		{
			status: "in-use",
			attachments: []volumes.Attachment{
				testAttachment(),
			},
		},
	})
	target := &OpenStack{
		ClientSet: &openstack.ClientSet{
			BlockStorage: blockStorage,
		},
	}

	err := target.waitForVolumeDetached(context.Background(), testVolumeID, testServerID, testLogger())
	if err == nil {
		t.Fatal("expected timeout while helper attachment remains")
	}
	if !strings.Contains(err.Error(), "helper_attached=true") {
		t.Fatalf("expected timeout to include helper attachment state, got %v", err)
	}
}

func TestWaitForVolumeDetachedReturnsOpenStackAPIError(t *testing.T) {
	withDetachPolling(t, 100*time.Millisecond, time.Millisecond)
	setFindVolumeDeviceResults(t, []deviceResult{{path: ""}})

	blockStorage, _ := newTestBlockStorageClient(t, testVolumeID, []testVolumeState{
		{statusCode: http.StatusInternalServerError},
	})
	target := &OpenStack{
		ClientSet: &openstack.ClientSet{
			BlockStorage: blockStorage,
		},
	}

	err := target.waitForVolumeDetached(context.Background(), testVolumeID, testServerID, testLogger())
	if err == nil {
		t.Fatal("expected OpenStack API error")
	}
}

func TestDisconnectTreatsImmediateDeviceDisappearanceAsSuccess(t *testing.T) {
	withDetachPolling(t, 100*time.Millisecond, time.Millisecond)
	setOpenStackDisconnectHooks(t)
	setFindVolumeDeviceResults(t, []deviceResult{{path: "/dev/vde"}, {path: ""}})

	blockStorage, _ := newTestBlockStorageClient(t, testVolumeID, []testVolumeState{
		{status: "available"},
	})
	var deleteCalls int
	deleteVolumeAttachment = func(context.Context, *gophercloud.ServiceClient, string, string) error {
		deleteCalls++
		return nil
	}

	target := newTestOpenStackTarget(blockStorage)
	err := target.Disconnect(context.Background())
	if err != nil {
		t.Fatalf("Disconnect() returned error: %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("expected one detach API call, got %d", deleteCalls)
	}
}

func TestDisconnectReturnsDetachAPIError(t *testing.T) {
	setOpenStackDisconnectHooks(t)
	setFindVolumeDeviceResults(t, []deviceResult{{path: "/dev/vde"}})

	detachErr := errors.New("detach API failed")
	deleteVolumeAttachment = func(context.Context, *gophercloud.ServiceClient, string, string) error {
		return detachErr
	}

	target := newTestOpenStackTarget(nil)
	err := target.Disconnect(context.Background())
	if !errors.Is(err, detachErr) {
		t.Fatalf("Disconnect() error = %v, want %v", err, detachErr)
	}
}

func TestWaitForVolumeDetachedDoesNotRetainStateBetweenDisks(t *testing.T) {
	withDetachPolling(t, 100*time.Millisecond, time.Millisecond)

	volumeIDs := []string{
		"cb562d94-3cd8-42b2-abf6-7e846a639df8",
		"9c2ca7ce-1ac8-4de4-a3af-95a8135b357c",
	}
	deviceLookups := []string{}
	findVolumeDevice = func(volumeID string) (string, error) {
		deviceLookups = append(deviceLookups, volumeID)
		return "", nil
	}
	t.Cleanup(func() {
		findVolumeDevice = findDevice
	})

	for _, volumeID := range volumeIDs {
		blockStorage, _ := newTestBlockStorageClient(t, volumeID, []testVolumeState{
			{status: "available"},
		})
		target := &OpenStack{
			ClientSet: &openstack.ClientSet{
				BlockStorage: blockStorage,
			},
		}

		err := target.waitForVolumeDetached(context.Background(), volumeID, testServerID, testLogger())
		if err != nil {
			t.Fatalf("waitForVolumeDetached(%q) returned error: %v", volumeID, err)
		}
	}

	if !reflect.DeepEqual(deviceLookups, volumeIDs) {
		t.Fatalf("device lookup order = %v, want %v", deviceLookups, volumeIDs)
	}
}

func testAttachment() volumes.Attachment {
	return volumes.Attachment{
		AttachmentID: "attachment-1",
		ID:           "attachment-id-fallback",
		ServerID:     testServerID,
		VolumeID:     testVolumeID,
		Device:       "/dev/vde",
	}
}

func withDetachPolling(t *testing.T, timeout time.Duration, interval time.Duration) {
	t.Helper()

	oldTimeout := volumeDetachTimeout
	oldInterval := volumeDetachPollInterval
	volumeDetachTimeout = timeout
	volumeDetachPollInterval = interval

	t.Cleanup(func() {
		volumeDetachTimeout = oldTimeout
		volumeDetachPollInterval = oldInterval
	})
}

func setFindVolumeDeviceResults(t *testing.T, results []deviceResult) {
	t.Helper()

	oldFindVolumeDevice := findVolumeDevice
	call := 0
	findVolumeDevice = func(string) (string, error) {
		if len(results) == 0 {
			return "", nil
		}

		resultIndex := call
		call++
		if resultIndex >= len(results) {
			resultIndex = len(results) - 1
		}

		result := results[resultIndex]
		return result.path, result.err
	}

	t.Cleanup(func() {
		findVolumeDevice = oldFindVolumeDevice
	})
}

func setOpenStackDisconnectHooks(t *testing.T) {
	t.Helper()

	oldGetTargetVolumeForDisk := getTargetVolumeForDisk
	oldGetCurrentInstanceUUID := getCurrentInstanceUUID
	oldDeleteVolumeAttachment := deleteVolumeAttachment

	getTargetVolumeForDisk = func(context.Context, *openstack.ClientSet, *object.VirtualMachine, *types.VirtualDisk) (*volumes.Volume, error) {
		return &volumes.Volume{
			ID:     testVolumeID,
			Status: "in-use",
			Attachments: []volumes.Attachment{
				testAttachment(),
			},
		}, nil
	}
	getCurrentInstanceUUID = func() (string, error) {
		return testServerID, nil
	}

	t.Cleanup(func() {
		getTargetVolumeForDisk = oldGetTargetVolumeForDisk
		getCurrentInstanceUUID = oldGetCurrentInstanceUUID
		deleteVolumeAttachment = oldDeleteVolumeAttachment
	})
}

func newTestOpenStackTarget(blockStorage *gophercloud.ServiceClient) *OpenStack {
	return &OpenStack{
		Disk: &types.VirtualDisk{
			VirtualDevice: types.VirtualDevice{
				Key: 2001,
			},
		},
		ClientSet: &openstack.ClientSet{
			BlockStorage: blockStorage,
			Compute:      &gophercloud.ServiceClient{},
		},
	}
}

func newTestBlockStorageClient(t *testing.T, volumeID string, states []testVolumeState) (*gophercloud.ServiceClient, *int) {
	t.Helper()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if len(states) == 0 {
			http.Error(w, "missing test volume state", http.StatusInternalServerError)
			return
		}

		stateIndex := requestCount - 1
		if stateIndex >= len(states) {
			stateIndex = len(states) - 1
		}

		state := states[stateIndex]
		if state.statusCode >= http.StatusBadRequest {
			http.Error(w, fmt.Sprintf(`{"error":{"code":%d,"title":"test error"}}`, state.statusCode), state.statusCode)
			return
		}

		status := state.status
		if status == "" {
			status = "available"
		}

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(map[string]any{
			"volume": map[string]any{
				"id":          volumeID,
				"status":      status,
				"attachments": state.attachments,
				"metadata":    map[string]string{},
			},
		})
		if err != nil {
			t.Errorf("failed to encode volume response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	provider := &gophercloud.ProviderClient{}
	provider.UseTokenLock()
	provider.SetToken("test-token")

	return &gophercloud.ServiceClient{
		ProviderClient: provider,
		Endpoint:       server.URL + "/",
		ResourceBase:   server.URL + "/",
		Type:           "volumev3",
	}, &requestCount
}

func testLogger() *log.Entry {
	logger := log.New()
	logger.SetOutput(os.Stderr)
	return log.NewEntry(logger)
}

func TestVolumeDetachedInOpenStack(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		attachmentCount int
		want            bool
	}{
		{
			name:   "available without attachments",
			status: "available",
			want:   true,
		},
		{
			name:            "available with attachment",
			status:          "available",
			attachmentCount: 1,
			want:            false,
		},
		{
			name:   "detaching without attachments",
			status: "detaching",
			want:   false,
		},
		{
			name:   "case insensitive available",
			status: "AVAILABLE",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := volumeDetachedInOpenStack(tt.status, tt.attachmentCount)
			if got != tt.want {
				t.Fatalf("volumeDetachedInOpenStack() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVolumeAttachComplete(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		attachmentCount int
		devicePath      string
		want            bool
	}{
		{
			name:            "in use with attachment and device",
			status:          "in-use",
			attachmentCount: 1,
			devicePath:      "/dev/sdb",
			want:            true,
		},
		{
			name:       "in use with device but no attachment",
			status:     "in-use",
			devicePath: "/dev/sdb",
			want:       false,
		},
		{
			name:            "in use with attachment but no device",
			status:          "in-use",
			attachmentCount: 1,
			want:            false,
		},
		{
			name:            "available with attachment and device",
			status:          "available",
			attachmentCount: 1,
			devicePath:      "/dev/sdb",
			want:            false,
		},
		{
			name:            "case insensitive in use",
			status:          "IN-USE",
			attachmentCount: 1,
			devicePath:      "/dev/sdb",
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := volumeAttachComplete(tt.status, tt.attachmentCount, tt.devicePath)
			if got != tt.want {
				t.Fatalf("volumeAttachComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVolumeAttachedInOpenStack(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		attachmentCount int
		want            bool
	}{
		{
			name:            "in use with attachment",
			status:          "in-use",
			attachmentCount: 1,
			want:            true,
		},
		{
			name:   "in use without attachment",
			status: "in-use",
			want:   false,
		},
		{
			name:            "attaching with attachment",
			status:          "attaching",
			attachmentCount: 1,
			want:            false,
		},
		{
			name:            "case insensitive in use",
			status:          "IN-USE",
			attachmentCount: 1,
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := volumeAttachedInOpenStack(tt.status, tt.attachmentCount)
			if got != tt.want {
				t.Fatalf("volumeAttachedInOpenStack() = %v, want %v", got, tt.want)
			}
		})
	}
}
