package vmware_nbdkit

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
	"github.com/vmware/govmomi/vim25/mo"
)

type libvirtDomain struct {
	XMLName  xml.Name        `xml:"domain"`
	Type     string          `xml:"type,attr"`
	Name     string          `xml:"name"`
	Memory   libvirtMemory   `xml:"memory"`
	VCPU     int32           `xml:"vcpu"`
	OS       libvirtOS       `xml:"os"`
	Features libvirtFeatures `xml:"features"`
	Devices  libvirtDevices  `xml:"devices"`
}

type libvirtMemory struct {
	Unit  string `xml:"unit,attr,omitempty"`
	Value int64  `xml:",chardata"`
}

type libvirtOS struct {
	Type libvirtOSType `xml:"type"`
	Boot libvirtBoot   `xml:"boot"`
}

type libvirtOSType struct {
	Value string `xml:",chardata"`
}

type libvirtBoot struct {
	Dev string `xml:"dev,attr"`
}

type libvirtFeatures struct {
	ACPI struct{} `xml:"acpi"`
	APIC struct{} `xml:"apic"`
	PAE  struct{} `xml:"pae"`
}

type libvirtDevices struct {
	Disks []libvirtDisk `xml:"disk"`
}

type libvirtDisk struct {
	Type   string            `xml:"type,attr"`
	Device string            `xml:"device,attr"`
	Driver libvirtDiskDriver `xml:"driver"`
	Source libvirtDiskSource `xml:"source"`
	Target libvirtDiskTarget `xml:"target"`
}

type libvirtDiskDriver struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type libvirtDiskSource struct {
	Dev string `xml:"dev,attr"`
}

type libvirtDiskTarget struct {
	Dev string `xml:"dev,attr"`
	Bus string `xml:"bus,attr"`
}

func linuxDiskName(index int) string {
	name := ""
	for {
		name = string(rune('a'+index%26)) + name
		index = index/26 - 1
		if index < 0 {
			break
		}
	}

	return "sd" + name
}

func (s *NbdkitServers) buildLibvirtXML(ctx context.Context, targets []*migrationTarget) ([]byte, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("no disks available for virt-v2v")
	}

	var vm mo.VirtualMachine
	err := s.VirtualMachine.Properties(ctx, s.VirtualMachine.Reference(), []string{"config"}, &vm)
	if err != nil {
		return nil, err
	}

	memoryKiB := int64(vm.Config.Hardware.MemoryMB) * 1024
	if memoryKiB == 0 {
		memoryKiB = 1048576
	}

	vcpu := vm.Config.Hardware.NumCPU
	if vcpu == 0 {
		vcpu = 1
	}

	domain := libvirtDomain{
		Type: "kvm",
		Name: vm.Config.Name,
		Memory: libvirtMemory{
			Unit:  "KiB",
			Value: memoryKiB,
		},
		VCPU: vcpu,
		OS: libvirtOS{
			Type: libvirtOSType{Value: "hvm"},
			Boot: libvirtBoot{Dev: "hd"},
		},
	}

	for index, target := range targets {
		domain.Devices.Disks = append(domain.Devices.Disks, libvirtDisk{
			Type:   "block",
			Device: "disk",
			Driver: libvirtDiskDriver{
				Name: "qemu",
				Type: "raw",
			},
			Source: libvirtDiskSource{
				Dev: target.Path,
			},
			Target: libvirtDiskTarget{
				Dev: linuxDiskName(index),
				Bus: "scsi",
			},
		})
	}

	body, err := xml.MarshalIndent(domain, "", "  ")
	if err != nil {
		return nil, err
	}

	return append([]byte(xml.Header), body...), nil
}

func (s *NbdkitServers) RunVirtV2VInPlace(ctx context.Context, targets []*migrationTarget) error {
	log.WithField("disks", len(targets)).Info("Running virt-v2v-in-place")

	xmlData, err := s.buildLibvirtXML(ctx, targets)
	if err != nil {
		return err
	}

	xmlFile, err := os.CreateTemp("", "migratekit-v2v-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(xmlFile.Name())

	_, err = xmlFile.Write(xmlData)
	if closeErr := xmlFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	args := []string{"-i", "libvirtxml", xmlFile.Name()}
	if s.VddkConfig.Debug {
		args = append([]string{"-v", "-x"}, args...)
	}

	cmd := exec.Command("virt-v2v-in-place", args...)
	cmd.Env = append(os.Environ(), "LIBGUESTFS_BACKEND=direct")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
