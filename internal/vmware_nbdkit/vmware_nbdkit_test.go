package vmware_nbdkit

import (
	"context"
	"errors"
	"testing"

	"github.com/vexxhost/migratekit/internal/vmware"
	"github.com/vmware/govmomi/vim25/types"
)

type fakeTarget struct {
	disk          *types.VirtualDisk
	changeID      *vmware.ChangeID
	getPathErr    error
	disconnectErr error
	disconnected  bool
}

func (t *fakeTarget) GetDisk() *types.VirtualDisk {
	return t.disk
}

func (t *fakeTarget) Connect(context.Context) error {
	return nil
}

func (t *fakeTarget) GetPath(context.Context) (string, error) {
	return "", t.getPathErr
}

func (t *fakeTarget) Disconnect(context.Context) error {
	t.disconnected = true
	return t.disconnectErr
}

func (t *fakeTarget) Exists(context.Context) (bool, error) {
	return true, nil
}

func (t *fakeTarget) GetCurrentChangeID(context.Context) (*vmware.ChangeID, error) {
	return t.changeID, nil
}

func (t *fakeTarget) WriteChangeID(context.Context, *vmware.ChangeID) error {
	return nil
}

func TestSyncToTargetReturnsDisconnectErrorAfterConnect(t *testing.T) {
	pathErr := errors.New("path error")
	disconnectErr := errors.New("disconnect error")
	disk := &types.VirtualDisk{
		VirtualDevice: types.VirtualDevice{
			Key: 2000,
			Backing: &types.VirtualDiskFlatVer2BackingInfo{
				ChangeId: "change-uuid/2",
			},
		},
	}

	target := &fakeTarget{
		disk: disk,
		changeID: &vmware.ChangeID{
			UUID:   "change-uuid",
			Number: "1",
			Value:  "change-uuid/1",
		},
		getPathErr:    pathErr,
		disconnectErr: disconnectErr,
	}

	server := &NbdkitServer{
		Disk: disk,
	}

	err := server.SyncToTarget(context.Background(), target, false)
	if !target.disconnected {
		t.Fatal("expected target to be disconnected")
	}
	if !errors.Is(err, pathErr) {
		t.Fatalf("expected returned error to include path error, got %v", err)
	}
	if !errors.Is(err, disconnectErr) {
		t.Fatalf("expected returned error to include disconnect error, got %v", err)
	}
}
