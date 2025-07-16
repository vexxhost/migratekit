package target

import (
	"context"
	"errors"
	"strconv"

	"github.com/francoisovh/migratekit/internal/vmware"
	"github.com/gosimple/slug"
	log "github.com/sirupsen/logrus"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

func DiskLabel(vm *object.VirtualMachine, disk *types.VirtualDisk) string {
	return slug.Make(vm.Name() + "-" + strconv.Itoa(int(disk.Key)))
}

func NeedsFullCopy(ctx context.Context, t Target) (bool, bool, error) {
	exists, err := t.Exists(ctx)
	if err != nil {
		return false, false, err
	}

	if !exists {
		log.Info("Data does not exist, full copy needed")

		return true, true, nil
	}

	currentChangeId, err := t.GetCurrentChangeID(ctx)
	if err != nil && !errors.Is(err, vmware.ErrInvalidChangeID) {
		return false, false, err
	}

	if currentChangeId == nil {
		log.Info("No or invalid change ID found, assuming full copy is needed")

		return true, false, nil
	}

	snapshotChangeId, err := vmware.GetChangeID(t.GetDisk())
	if err != nil {
		return false, false, err
	}

	if currentChangeId.UUID != snapshotChangeId.UUID {
		log.WithFields(log.Fields{
			"currentChangeId":  currentChangeId.Value,
			"snapshotChangeId": snapshotChangeId.Value,
		}).Warning("Change ID mismatch, full copy needed")

		return true, false, nil
	}

	log.Info("Starting incremental copy")

	return false, false, nil
}
