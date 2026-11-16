package vmware_nbdkit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vexxhost/migratekit/internal/nbdkit"
	"github.com/vexxhost/migratekit/internal/target"
	"github.com/vexxhost/migratekit/internal/vmware"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

type copyTarget struct {
	target.Target
	exists        bool
	checkpoint    *vmware.ChangeID
	disconnectErr error
	disconnected  bool
}

func (t *copyTarget) Exists(context.Context) (bool, error)    { return t.exists, nil }
func (t *copyTarget) Connect(context.Context) error           { t.exists = true; return nil }
func (t *copyTarget) GetPath(context.Context) (string, error) { return "/dev/test-target", nil }
func (t *copyTarget) GetCurrentChangeID(context.Context) (*vmware.ChangeID, error) {
	return t.checkpoint, nil
}
func (t *copyTarget) GetDisk() *types.VirtualDisk {
	return &types.VirtualDisk{VirtualDevice: types.VirtualDevice{
		Backing: &types.VirtualDiskFlatVer2BackingInfo{ChangeId: "epoch/2"},
	}, CapacityInBytes: 4096}
}
func (t *copyTarget) WriteChangeID(_ context.Context, id *vmware.ChangeID) error {
	t.checkpoint = id
	return nil
}
func (t *copyTarget) Disconnect(context.Context) error {
	t.disconnected = true
	return t.disconnectErr
}

func TestFullSyncUsesZeroOptimizationOnlyForFreshVolumes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nbdcopy"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$MIGRATEKIT_TEST_ARGS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	argsFile := filepath.Join(dir, "args")
	t.Setenv("MIGRATEKIT_TEST_ARGS", argsFile)
	for _, exists := range []bool{false, true} {
		destination := &copyTarget{exists: exists}
		nbd, err := nbdkit.NewNbdkitBuilder().Build()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(filepath.Dir(nbd.Socket())) })
		vm := object.NewVirtualMachine(nil, types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-test"})
		vm.InventoryPath = "/dc/vm/test-guest"
		server := &NbdkitServer{Servers: &NbdkitServers{VirtualMachine: vm}, Disk: destination.GetDisk(), Nbdkit: nbd}
		if err := server.SyncToTarget(context.Background(), destination); err != nil {
			t.Fatal(err)
		}
		args, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatal(err)
		}
		if got := slices.Contains(strings.Fields(string(args)), "--destination-is-zero"); got == exists {
			t.Fatalf("existing=%v nbdcopy arguments=%s", exists, args)
		}
		if !destination.disconnected {
			t.Fatal("destination remains connected")
		}
	}
}

func TestConnectRetainsCopyStateBeforeCreatingVolume(t *testing.T) {
	for _, tc := range []struct {
		name        string
		exists      bool
		checkpoint  *vmware.ChangeID
		full, clean bool
	}{
		{"new zeroed volume", false, nil, true, true},
		{"existing modified volume", true, nil, true, false},
		{"existing checkpoint", true, &vmware.ChangeID{UUID: "epoch", Value: "epoch/1"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			destination := &copyTarget{exists: tc.exists, checkpoint: tc.checkpoint}
			mt := &migrationTarget{Target: destination}
			if err := mt.connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			if mt.NeedFullCopy != tc.full || mt.TargetIsClean != tc.clean {
				t.Fatalf("copy state full=%v clean=%v; want full=%v clean=%v", mt.NeedFullCopy, mt.TargetIsClean, tc.full, tc.clean)
			}
		})
	}
}

func TestDisconnectContinuesAfterEarlierFailure(t *testing.T) {
	want := errors.New("detach failed")
	first, last := &copyTarget{disconnectErr: want}, &copyTarget{}
	err := disconnectMigrationTargets(context.Background(), []*migrationTarget{{Target: first}, {Target: last}})
	if !errors.Is(err, want) || !first.disconnected || !last.disconnected {
		t.Fatalf("cleanup stopped early or lost failure: err=%v first=%v last=%v", err, first.disconnected, last.disconnected)
	}
}
