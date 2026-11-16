package vmware

import (
	"reflect"
	"testing"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

func TestDisksInBootOrder(t *testing.T) {
	devices := object.VirtualDeviceList{
		&types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2000}},
		&types.VirtualVmxnet3{},
		&types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2001}},
		&types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2002}},
	}
	boot := func(key int32) types.BaseVirtualMachineBootOptionsBootableDevice {
		return &types.VirtualMachineBootOptionsBootableDiskDevice{DeviceKey: key}
	}
	for _, test := range []struct {
		name    string
		options *types.VirtualMachineBootOptions
		want    []int32
	}{
		{"default", nil, []int32{2000, 2001, 2002}},
		{"empty", &types.VirtualMachineBootOptions{}, []int32{2000, 2001, 2002}},
		{"second disk boots", &types.VirtualMachineBootOptions{BootOrder: []types.BaseVirtualMachineBootOptionsBootableDevice{boot(2001)}}, []int32{2001, 2000, 2002}},
		{"stale and duplicate entries", &types.VirtualMachineBootOptions{BootOrder: []types.BaseVirtualMachineBootOptionsBootableDevice{boot(9999), boot(2002), boot(2002), &types.VirtualMachineBootOptionsBootableCdromDevice{}, boot(2001)}}, []int32{2002, 2001, 2000}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got []int32
			for _, disk := range DisksInBootOrder(devices, test.options) {
				got = append(got, disk.Key)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
	if devices[0].GetVirtualDevice().Key != 2000 {
		t.Fatal("source devices were reordered")
	}
}
