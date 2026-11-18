package vmware

import (
	"strings"
	"testing"

	"github.com/vmware/govmomi/vim25/types"
)

func TestValidateSnapshotDisks(t *testing.T) {
	for _, mode := range []string{"persistent", "nonpersistent", "independent_persistent", "independent_nonpersistent"} {
		for name, backing := range map[string]types.BaseVirtualDeviceBackingInfo{
			"flat-v1":   &types.VirtualDiskFlatVer1BackingInfo{DiskMode: mode},
			"flat-v2":   &types.VirtualDiskFlatVer2BackingInfo{DiskMode: mode},
			"sparse-v1": &types.VirtualDiskSparseVer1BackingInfo{DiskMode: mode},
			"sparse-v2": &types.VirtualDiskSparseVer2BackingInfo{DiskMode: mode},
			"sesparse":  &types.VirtualDiskSeSparseBackingInfo{DiskMode: mode},
			"rdm":       &types.VirtualDiskRawDiskMappingVer1BackingInfo{DiskMode: mode},
		} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				devices := []types.BaseVirtualDevice{
					&types.VirtualCdrom{},
					&types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2000, Backing: &types.VirtualDiskFlatVer2BackingInfo{DiskMode: "persistent"}}},
					&types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2001, Backing: backing}},
				}
				err := ValidateSnapshotDisks(devices)
				if strings.HasPrefix(mode, "independent_") {
					if err == nil || !strings.Contains(err.Error(), "disk 2001") || !strings.Contains(err.Error(), mode) || !strings.Contains(err.Error(), "excluded from snapshots") {
						t.Fatalf("expected actionable independent-disk rejection, got %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
