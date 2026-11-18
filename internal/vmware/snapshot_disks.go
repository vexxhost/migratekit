package vmware

import (
	"fmt"
	"strings"

	"github.com/vmware/govmomi/vim25/types"
)

// ValidateSnapshotDisks rejects disks whose writes are excluded from snapshots.
// Do this before creating destination resources or a migration snapshot.
func ValidateSnapshotDisks(devices []types.BaseVirtualDevice) error {
	for _, device := range devices {
		disk, ok := device.(*types.VirtualDisk)
		if !ok {
			continue
		}
		var mode string
		switch backing := disk.Backing.(type) {
		case *types.VirtualDiskFlatVer1BackingInfo:
			mode = backing.DiskMode
		case *types.VirtualDiskFlatVer2BackingInfo:
			mode = backing.DiskMode
		case *types.VirtualDiskSparseVer1BackingInfo:
			mode = backing.DiskMode
		case *types.VirtualDiskSparseVer2BackingInfo:
			mode = backing.DiskMode
		case *types.VirtualDiskSeSparseBackingInfo:
			mode = backing.DiskMode
		case *types.VirtualDiskRawDiskMappingVer1BackingInfo:
			mode = backing.DiskMode
		}
		if strings.HasPrefix(mode, "independent_") {
			return fmt.Errorf("disk %d uses %s mode and is excluded from snapshots; independent disks are not supported for migration: prepare a snapshot-capable copy before retrying", disk.Key, mode)
		}
	}
	return nil
}
