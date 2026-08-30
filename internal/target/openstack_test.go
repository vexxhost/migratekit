package target

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

func testContext() context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, "volumeCreateOpts", &VolumeCreateOpts{})
	return ctx
}

func testTarget() *OpenStack {
	return &OpenStack{
		ClientSet: &openstack.ClientSet{},
		VirtualMachine: object.NewVirtualMachine(nil, types.ManagedObjectReference{
			Type:  "VirtualMachine",
			Value: "vm-123",
		}),
		Disk: &types.VirtualDisk{
			VirtualDevice: types.VirtualDevice{
				Key: 2000,
			},
		},
	}
}

func stubVolumeHooks(t *testing.T, volume *volumes.Volume) {
	t.Helper()

	oldGetVolumeForDisk := getVolumeForDisk
	oldGetCurrentInstanceUUID := getCurrentInstanceUUID
	oldFindVolumeDevice := findVolumeDevice
	oldAttachVolume := attachVolume
	oldDetachVolume := detachVolume
	oldWaitVolumeStatus := waitVolumeStatus
	oldAttachPollTimeout := attachPollTimeout
	oldAttachPollEvery := attachPollEvery

	getVolumeForDisk = func(context.Context, *openstack.ClientSet, *object.VirtualMachine, *types.VirtualDisk) (*volumes.Volume, error) {
		return volume, nil
	}
	getCurrentInstanceUUID = func() (string, error) {
		return "", errors.New("unexpected metadata lookup")
	}
	findVolumeDevice = func(string) (string, error) {
		return "", errors.New("unexpected device lookup")
	}
	attachVolume = func(context.Context, *OpenStack, string, string) error {
		return errors.New("unexpected volume attach")
	}
	detachVolume = func(context.Context, *OpenStack, string, string) error {
		return errors.New("unexpected volume detach")
	}
	waitVolumeStatus = func(context.Context, *gophercloud.ServiceClient, string, string) error {
		return errors.New("unexpected volume status wait")
	}
	attachPollTimeout = time.Second
	attachPollEvery = time.Millisecond

	t.Cleanup(func() {
		getVolumeForDisk = oldGetVolumeForDisk
		getCurrentInstanceUUID = oldGetCurrentInstanceUUID
		findVolumeDevice = oldFindVolumeDevice
		attachVolume = oldAttachVolume
		detachVolume = oldDetachVolume
		waitVolumeStatus = oldWaitVolumeStatus
		attachPollTimeout = oldAttachPollTimeout
		attachPollEvery = oldAttachPollEvery
	})
}

func TestConnectPreAttachedVolumeSkipsMetadataAndAttach(t *testing.T) {
	volume := &volumes.Volume{ID: "volume-123"}
	stubVolumeHooks(t, volume)

	var deviceLookups int
	findVolumeDevice = func(volumeID string) (string, error) {
		deviceLookups++
		if volumeID != volume.ID {
			t.Fatalf("volume ID = %q, want %q", volumeID, volume.ID)
		}
		return "/dev/sdb", nil
	}

	target := testTarget()
	if err := target.Connect(testContext()); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	if deviceLookups != 1 {
		t.Fatalf("device lookups = %d, want 1", deviceLookups)
	}
	if target.attachedInstanceUUID != "" || target.attachedVolumeID != "" {
		t.Fatalf("target recorded attachment ownership: instance=%q volume=%q", target.attachedInstanceUUID, target.attachedVolumeID)
	}
}

func TestConnectAutoAttachUsesMetadataAndRecordsOwnership(t *testing.T) {
	volume := &volumes.Volume{ID: "volume-123"}
	stubVolumeHooks(t, volume)

	var deviceLookups int
	findVolumeDevice = func(volumeID string) (string, error) {
		deviceLookups++
		if volumeID != volume.ID {
			t.Fatalf("volume ID = %q, want %q", volumeID, volume.ID)
		}
		if deviceLookups == 1 {
			return "", nil
		}
		return "/dev/sdb", nil
	}

	var metadataLookups int
	getCurrentInstanceUUID = func() (string, error) {
		metadataLookups++
		return "instance-123", nil
	}

	var attachedInstanceUUID, attachedVolumeID string
	attachVolume = func(_ context.Context, _ *OpenStack, instanceUUID, volumeID string) error {
		attachedInstanceUUID = instanceUUID
		attachedVolumeID = volumeID
		return nil
	}

	target := testTarget()
	if err := target.Connect(testContext()); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	if metadataLookups != 1 {
		t.Fatalf("metadata lookups = %d, want 1", metadataLookups)
	}
	if attachedInstanceUUID != "instance-123" || attachedVolumeID != volume.ID {
		t.Fatalf("attached instance=%q volume=%q, want instance-123/%s", attachedInstanceUUID, attachedVolumeID, volume.ID)
	}
	if target.attachedInstanceUUID != "instance-123" || target.attachedVolumeID != volume.ID {
		t.Fatalf("target ownership instance=%q volume=%q, want instance-123/%s", target.attachedInstanceUUID, target.attachedVolumeID, volume.ID)
	}
}

func TestDisconnectPreAttachedVolumeDoesNotDetach(t *testing.T) {
	volume := &volumes.Volume{ID: "volume-123"}
	stubVolumeHooks(t, volume)

	target := testTarget()
	if err := target.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect returned error: %v", err)
	}
}

func TestDisconnectAutoAttachedVolumeDetachesSavedAttachment(t *testing.T) {
	volume := &volumes.Volume{ID: "volume-123"}
	stubVolumeHooks(t, volume)

	var detachedInstanceUUID, detachedVolumeID string
	detachVolume = func(_ context.Context, _ *OpenStack, instanceUUID, volumeID string) error {
		detachedInstanceUUID = instanceUUID
		detachedVolumeID = volumeID
		return nil
	}

	var waitedVolumeID, waitedStatus string
	waitVolumeStatus = func(_ context.Context, _ *gophercloud.ServiceClient, volumeID, status string) error {
		waitedVolumeID = volumeID
		waitedStatus = status
		return nil
	}

	target := testTarget()
	target.attachedInstanceUUID = "instance-123"
	target.attachedVolumeID = volume.ID

	if err := target.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect returned error: %v", err)
	}

	if detachedInstanceUUID != "instance-123" || detachedVolumeID != volume.ID {
		t.Fatalf("detached instance=%q volume=%q, want instance-123/%s", detachedInstanceUUID, detachedVolumeID, volume.ID)
	}
	if waitedVolumeID != volume.ID || waitedStatus != "available" {
		t.Fatalf("waited for volume=%q status=%q, want %s/available", waitedVolumeID, waitedStatus, volume.ID)
	}
	if target.attachedInstanceUUID != "" || target.attachedVolumeID != "" {
		t.Fatalf("target ownership was not cleared: instance=%q volume=%q", target.attachedInstanceUUID, target.attachedVolumeID)
	}
}
