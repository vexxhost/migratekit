package vmware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

const migrationLeaseKey = "migratekit.migrationLock"

var ErrMigrationLocked = errors.New("source VM is locked by another migration; stop the previous migration before using --recover-source-lock")
var ErrMigrationLeaseOwnership = errors.New("source migration lease ownership changed; refusing to clear another migration's lease")

type MigrationLease struct {
	vm       *object.VirtualMachine
	owner    string
	pending  *object.Task
	acquired bool
}

func migrationLeaseState(ctx context.Context, vm *object.VirtualMachine) (string, string, error) {
	var state mo.VirtualMachine
	if err := vm.Properties(ctx, vm.Reference(), []string{"config.changeVersion", "config.extraConfig"}, &state); err != nil {
		return "", "", err
	}
	if state.Config == nil || state.Config.ChangeVersion == "" {
		return "", "", errors.New("source configuration version is unavailable for migration locking")
	}
	owner := ""
	for _, option := range state.Config.ExtraConfig {
		item := option.GetOptionValue()
		if item.Key == migrationLeaseKey {
			var ok bool
			owner, ok = item.Value.(string)
			if !ok {
				return "", "", errors.New("source migration lease has an invalid value; manual review required")
			}
		}
	}
	return state.Config.ChangeVersion, owner, nil
}

// ChangeVersion makes competing claims conditional on the same configuration
// version. A failed claim must never clear the successful claimant's marker.
func AcquireMigrationLease(ctx context.Context, vm *object.VirtualMachine, recover bool) (*MigrationLease, error) {
	version, owner, err := migrationLeaseState(ctx, vm)
	if err != nil {
		return nil, err
	}
	if owner != "" && !recover {
		return nil, ErrMigrationLocked
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	lease := &MigrationLease{vm: vm, owner: hex.EncodeToString(nonce[:])}
	task, err := vm.Reconfigure(ctx, types.VirtualMachineConfigSpec{ChangeVersion: version, ExtraConfig: []types.BaseOptionValue{&types.OptionValue{Key: migrationLeaseKey, Value: lease.owner}}})
	if err != nil {
		return nil, fmt.Errorf("claiming source migration lease: %w", err)
	}
	lease.pending = task
	waitErr := task.Wait(ctx)
	var failed interface{ Fault() types.BaseMethodFault }
	if errors.As(waitErr, &failed) {
		return nil, fmt.Errorf("claiming source migration lease: %w", waitErr)
	}
	if err = errors.Join(waitErr, ctx.Err()); err != nil {
		return lease, fmt.Errorf("claiming source migration lease: %w", err)
	}
	lease.acquired = true
	return lease, nil
}

func (l *MigrationLease) Release(ctx context.Context) error {
	if !l.acquired {
		waitErr := l.pending.Wait(ctx)
		var failed interface{ Fault() types.BaseMethodFault }
		if errors.As(waitErr, &failed) {
			return nil // A definitively failed claim never owned the marker.
		}
		if err := errors.Join(waitErr, ctx.Err()); err != nil {
			return fmt.Errorf("settling source migration lease acquisition: %w", err)
		}
		l.acquired = true
	}
	version, owner, err := migrationLeaseState(ctx, l.vm)
	if err != nil {
		return err
	}
	if owner == "" {
		return nil
	}
	if owner != l.owner {
		return ErrMigrationLeaseOwnership
	}
	task, err := l.vm.Reconfigure(ctx, types.VirtualMachineConfigSpec{ChangeVersion: version, ExtraConfig: []types.BaseOptionValue{&types.OptionValue{Key: migrationLeaseKey, Value: ""}}})
	if err != nil {
		return err
	}
	return errors.Join(task.Wait(ctx), ctx.Err())
}
