package vmware

import (
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// DisksInBootOrder puts explicitly selected boot disks first. Guests without
// an explicit disk boot order retain their existing device order.
func DisksInBootOrder(devices object.VirtualDeviceList, options *types.VirtualMachineBootOptions) []*types.VirtualDisk {
	var disks []*types.VirtualDisk
	byKey := make(map[int32]*types.VirtualDisk)
	for _, device := range devices {
		if disk, ok := device.(*types.VirtualDisk); ok {
			disks = append(disks, disk)
			byKey[disk.Key] = disk
		}
	}
	if options == nil || len(options.BootOrder) == 0 {
		return disks
	}
	ordered := make([]*types.VirtualDisk, 0, len(disks))
	seen := make(map[int32]bool)
	for _, device := range options.BootOrder {
		bootDisk, ok := device.(*types.VirtualMachineBootOptionsBootableDiskDevice)
		if !ok {
			continue
		}
		if disk := byKey[bootDisk.DeviceKey]; disk != nil && !seen[disk.Key] {
			ordered = append(ordered, disk)
			seen[disk.Key] = true
		}
	}
	for _, disk := range disks {
		if !seen[disk.Key] {
			ordered = append(ordered, disk)
		}
	}
	return ordered
}
