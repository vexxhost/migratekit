package vmware

import (
	"context"
	"errors"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
)

func leaseFixture(t *testing.T) (*govmomi.Client, *object.VirtualMachine) {
	t.Helper()
	ctx := context.Background()
	model := simulator.VPX()
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.Remove)
	api := model.Service.NewServer()
	t.Cleanup(api.Close)
	client, err := govmomi.NewClient(ctx, api.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Logout(ctx) })
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
	return client, vms[0]
}

func TestMigrationLeaseOwnershipAndExplicitRecovery(t *testing.T) {
	_, vm := leaseFixture(t)
	ctx := context.Background()
	first, err := AcquireMigrationLease(ctx, vm, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireMigrationLease(ctx, vm, false); !errors.Is(err, ErrMigrationLocked) {
		t.Fatalf("active source accepted: %v", err)
	}
	second, err := AcquireMigrationLease(ctx, vm, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.owner == first.owner {
		t.Fatal("recovery reused previous ownership")
	}
	if err := first.Release(ctx); !errors.Is(err, ErrMigrationLeaseOwnership) {
		t.Fatalf("old owner cleared replacement lease: %v", err)
	}
	_, owner, err := migrationLeaseState(ctx, vm)
	if err != nil || owner != second.owner {
		t.Fatal("replacement marker changed")
	}
	if err := second.Release(ctx); err != nil {
		t.Fatal(err)
	}
	third, err := AcquireMigrationLease(ctx, vm, false)
	if err != nil {
		t.Fatalf("completed migration still locked: %v", err)
	}
	if err := third.Release(ctx); err != nil {
		t.Fatal(err)
	}
}

type canceledLeaseRequest struct {
	soap.RoundTripper
	cancel context.CancelFunc
	t      *testing.T
}

func (r canceledLeaseRequest) RoundTrip(ctx context.Context, req, res soap.HasFault) error {
	if body, ok := req.(*methods.ReconfigVM_TaskBody); ok && body.Req.Spec.ChangeVersion == "" {
		r.t.Error("lease write missing configuration-version condition")
	}
	err := r.RoundTripper.RoundTrip(ctx, req, res)
	if _, ok := req.(*methods.ReconfigVM_TaskBody); ok && err == nil {
		r.cancel()
	}
	return err
}

func TestCanceledLeaseClaimCanBeSettledAndReleased(t *testing.T) {
	client, vm := leaseFixture(t)
	ctx := context.Background()
	previous := simulator.TaskDelay
	simulator.TaskDelay = simulator.DelayConfig{MethodDelay: map[string]int{"ReconfigVM_Task": 100, "LockHandoff": 0}}
	defer func() { simulator.TaskDelay = previous }()
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	client.Client.RoundTripper = canceledLeaseRequest{RoundTripper: client.Client.RoundTripper, cancel: cancel, t: t}
	lease, err := AcquireMigrationLease(canceled, vm, false)
	if !errors.Is(err, context.Canceled) || lease == nil {
		t.Fatalf("lost accepted claim after cancellation: lease=%v err=%v", lease, err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
	_, owner, err := migrationLeaseState(ctx, vm)
	if err != nil || owner != "" {
		t.Fatalf("canceled claim leaked its marker: owner=%q err=%v", owner, err)
	}
}
