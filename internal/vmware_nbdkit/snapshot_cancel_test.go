package vmware_nbdkit

import (
	"context"
	"errors"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
)

type cancelSnapshotRequest struct {
	soap.RoundTripper
	cancel context.CancelFunc
}

func (r cancelSnapshotRequest) RoundTrip(ctx context.Context, req, res soap.HasFault) error {
	err := r.RoundTripper.RoundTrip(ctx, req, res)
	if _, ok := req.(*methods.CreateSnapshot_TaskBody); ok && err == nil {
		r.cancel()
	}
	return err
}

func TestCanceledSnapshotTaskRemainsOwnedForCleanup(t *testing.T) {
	model := simulator.VPX()
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	defer model.Remove()
	api := model.Service.NewServer()
	defer api.Close()
	ctx := context.Background()
	client, err := govmomi.NewClient(ctx, api.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Logout(ctx)
	finder := find.NewFinder(client.Client, true)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	finder.SetDatacenter(dc)
	vms, err := finder.VirtualMachineList(ctx, "*")
	if err != nil || len(vms) == 0 {
		t.Fatalf("VM fixture: %v", err)
	}
	previous := simulator.TaskDelay
	simulator.TaskDelay = simulator.DelayConfig{MethodDelay: map[string]int{"CreateSnapshot_Task": 200, "LockHandoff": 0}}
	defer func() { simulator.TaskDelay = previous }()
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	client.Client.RoundTripper = cancelSnapshotRequest{RoundTripper: client.Client.RoundTripper, cancel: cancel}
	servers := &NbdkitServers{VirtualMachine: vms[0]}
	err = servers.createSnapshot(canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if servers.SnapshotRef.Value == "" {
		t.Fatal("accepted snapshot task lost its result and cannot be cleaned up")
	}
	if err := servers.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if ref, _ := vms[0].FindSnapshot(ctx, "migratekit"); ref != nil {
		t.Fatal("snapshot remains after cleanup")
	}
}
