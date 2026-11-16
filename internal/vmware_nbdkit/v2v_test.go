package vmware_nbdkit

import (
	"encoding/xml"
	"fmt"
	"testing"

	"github.com/vmware/govmomi/vim25/types"
)

func TestBuildDomainXMLIncludesEveryDisk(t *testing.T) {
	config := &types.VirtualMachineConfigInfo{
		Name:     "guest <multi-disk>",
		Hardware: types.VirtualHardware{MemoryMB: 2048, NumCPU: 2},
	}
	var targets []*migrationTarget
	for i := 0; i < 28; i++ {
		targets = append(targets, &migrationTarget{Path: fmt.Sprintf("/dev/test-disk-%d", i)})
	}
	data, err := buildDomainXML(config, targets)
	if err != nil {
		t.Fatal(err)
	}
	var domain libvirtDomain
	if err := xml.Unmarshal(data, &domain); err != nil {
		t.Fatal(err)
	}
	if domain.Name != config.Name || domain.Memory.Value != 2048*1024 || domain.VCPU != 2 {
		t.Fatalf("guest configuration lost: %+v", domain)
	}
	if len(domain.Devices.Disks) != len(targets) {
		t.Fatalf("got %d disks, want %d", len(domain.Devices.Disks), len(targets))
	}
	for i, disk := range domain.Devices.Disks {
		if disk.Source.Dev != targets[i].Path || disk.Type != "block" || disk.Driver.Type != "raw" {
			t.Fatalf("disk %d: %+v", i, disk)
		}
	}
	if domain.Devices.Disks[0].Target.Dev != "sda" || domain.Devices.Disks[26].Target.Dev != "sdaa" || domain.Devices.Disks[27].Target.Dev != "sdab" {
		t.Fatal("disk names must remain unique beyond 26 disks")
	}
}

func TestBuildDomainXMLRejectsNoDisks(t *testing.T) {
	if _, err := buildDomainXML(&types.VirtualMachineConfigInfo{}, nil); err == nil {
		t.Fatal("expected an error when no disks are attached")
	}
}
