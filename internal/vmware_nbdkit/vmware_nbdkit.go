package vmware_nbdkit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/vexxhost/migratekit/internal/nbdcopy"
	"github.com/vexxhost/migratekit/internal/nbdkit"
	"github.com/vexxhost/migratekit/internal/progress"
	"github.com/vexxhost/migratekit/internal/target"
	"github.com/vexxhost/migratekit/internal/vmware"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"libguestfs.org/libnbd"
)

const MaxChunkSize = 64 * 1024 * 1024

type VddkConfig struct {
	Debug       bool
	Endpoint    *url.URL
	Thumbprint  string
	Compression nbdkit.CompressionMethod
}

type NbdkitServers struct {
	VddkConfig     *VddkConfig
	VirtualMachine *object.VirtualMachine
	SnapshotRef    types.ManagedObjectReference
	Servers        []*NbdkitServer
}

type NbdkitServer struct {
	Servers *NbdkitServers
	Disk    *types.VirtualDisk
	Nbdkit  *nbdkit.NbdkitServer
}

func NewNbdkitServers(vddk *VddkConfig, vm *object.VirtualMachine) *NbdkitServers {
	return &NbdkitServers{
		VddkConfig:     vddk,
		VirtualMachine: vm,
		Servers:        []*NbdkitServer{},
	}
}

func (s *NbdkitServers) createSnapshot(ctx context.Context) error {
	task, err := s.VirtualMachine.CreateSnapshot(ctx, "migratekit", "Ephemeral snapshot for MigrateKit", false, false)
	if err != nil {
		return err
	}

	bar := progress.NewVMwareProgressBar("Creating snapshot")
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		bar.Loop(ctx.Done())
	}()
	defer cancel()

	info, err := task.WaitForResult(ctx, bar)
	if err != nil {
		return err
	}

	s.SnapshotRef = info.Result.(types.ManagedObjectReference)
	return nil
}

func (s *NbdkitServers) Start(ctx context.Context) error {
	err := s.createSnapshot(ctx)
	if err != nil {
		return err
	}

	var snapshot mo.VirtualMachineSnapshot
	err = s.VirtualMachine.Properties(ctx, s.SnapshotRef, []string{"config.hardware"}, &snapshot)
	if err != nil {
		return err
	}

	for _, device := range snapshot.Config.Hardware.Device {
		switch disk := device.(type) {
		case *types.VirtualDisk:
			backing := disk.Backing.(types.BaseVirtualDeviceFileBackingInfo)
			info := backing.GetVirtualDeviceFileBackingInfo()

			password, _ := s.VddkConfig.Endpoint.User.Password()
			server, err := nbdkit.NewNbdkitBuilder().
				Server(s.VddkConfig.Endpoint.Host).
				Username(s.VddkConfig.Endpoint.User.Username()).
				Password(password).
				Thumbprint(s.VddkConfig.Thumbprint).
				VirtualMachine(s.VirtualMachine.Reference().Value).
				Snapshot(s.SnapshotRef.Value).
				Filename(info.FileName).
				Compression(s.VddkConfig.Compression).
				Build()
			if err != nil {
				return err
			}

			if err := server.Start(); err != nil {
				return err
			}

			s.Servers = append(s.Servers, &NbdkitServer{
				Servers: s,
				Disk:    disk,
				Nbdkit:  server,
			})
		}
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Warn("Received interrupt signal, cleaning up...")

		err := s.Stop(ctx)
		if err != nil {
			log.WithError(err).Fatal("Failed to stop nbdkit servers")
		}

		os.Exit(1)
	}()

	return nil
}

func (s *NbdkitServers) removeSnapshot(ctx context.Context) error {
	consolidate := true
	task, err := s.VirtualMachine.RemoveSnapshot(ctx, s.SnapshotRef.Value, false, &consolidate)
	if err != nil {
		return err
	}

	bar := progress.NewVMwareProgressBar("Removing snapshot")
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		bar.Loop(ctx.Done())
	}()
	defer cancel()

	_, err = task.WaitForResult(ctx, bar)
	if err != nil {
		return err
	}

	return nil
}

func (s *NbdkitServers) Stop(ctx context.Context) error {
	for _, server := range s.Servers {
		if err := server.Nbdkit.Stop(); err != nil {
			return err
		}
	}

	err := s.removeSnapshot(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (s *NbdkitServers) MigrationCycle(ctx context.Context, runV2V bool) error {
	err := s.Start(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err := s.Stop(ctx)
		if err != nil {
			log.WithError(err).Fatal("Failed to stop nbdkit servers")
		}
	}()

	fixNicNames, _ := ctx.Value("fixNicNames").(bool)

	for index, server := range s.Servers {
		t, err := target.NewOpenStack(ctx, s.VirtualMachine, server.Disk)
		if err != nil {
			return err
		}

		if index != 0 {
			runV2V = false
		}

		err = server.SyncToTarget(ctx, t, runV2V, fixNicNames && index == 0)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *NbdkitServer) FullCopyToTarget(t target.Target, path string, targetIsClean bool) error {
	logger := log.WithFields(log.Fields{
		"vm":   s.Servers.VirtualMachine.Name(),
		"disk": s.Disk.Backing.(types.BaseVirtualDeviceFileBackingInfo).GetVirtualDeviceFileBackingInfo().FileName,
	})

	logger.Info("Starting full copy")

	err := nbdcopy.Run(
		s.Nbdkit.LibNBDExportName(),
		path,
		s.Disk.CapacityInBytes,
		targetIsClean,
	)
	if err != nil {
		return err
	}

	logger.Info("Full copy completed")

	return nil
}

func (s *NbdkitServer) IncrementalCopyToTarget(ctx context.Context, t target.Target, path string) error {
	logger := log.WithFields(log.Fields{
		"vm":   s.Servers.VirtualMachine.Name(),
		"disk": s.Disk.Backing.(types.BaseVirtualDeviceFileBackingInfo).GetVirtualDeviceFileBackingInfo().FileName,
	})

	logger.Info("Starting incremental copy")

	currentChangeId, err := t.GetCurrentChangeID(ctx)
	if err != nil {
		return err
	}

	handle, err := libnbd.Create()
	if err != nil {
		return err
	}

	err = handle.ConnectUri(s.Nbdkit.LibNBDExportName())
	if err != nil {
		return err
	}

	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_EXCL|syscall.O_DIRECT, 0644)
	if err != nil {
		return err
	}
	defer fd.Close()

	startOffset := int64(0)
	bar := progress.DataProgressBar("Incremental copy", s.Disk.CapacityInBytes)

	for {
		req := types.QueryChangedDiskAreas{
			This:        s.Servers.VirtualMachine.Reference(),
			Snapshot:    &s.Servers.SnapshotRef,
			DeviceKey:   s.Disk.Key,
			StartOffset: startOffset,
			ChangeId:    currentChangeId.Value,
		}

		res, err := methods.QueryChangedDiskAreas(ctx, s.Servers.VirtualMachine.Client(), &req)
		if err != nil {
			return err
		}

		diskChangeInfo := res.Returnval

		for _, area := range diskChangeInfo.ChangedArea {
			for offset := area.Start; offset < area.Start+area.Length; {
				chunkSize := area.Length - (offset - area.Start)
				if chunkSize > MaxChunkSize {
					chunkSize = MaxChunkSize
				}

				buf := make([]byte, chunkSize)
				err = handle.Pread(buf, uint64(offset), nil)
				if err != nil {
					return err
				}

				_, err = fd.WriteAt(buf, offset)
				if err != nil {
					return err
				}

				bar.Set64(offset + chunkSize)
				offset += chunkSize
			}
		}

		startOffset = diskChangeInfo.StartOffset + diskChangeInfo.Length
		bar.Set64(startOffset)

		if startOffset == s.Disk.CapacityInBytes {
			break
		}
	}

	return nil
}

func (s *NbdkitServer) SyncToTarget(ctx context.Context, t target.Target, runV2V bool, fixNicNames bool) error {
	snapshotChangeId, err := vmware.GetChangeID(s.Disk)
	if err != nil {
		if !errors.Is(err, vmware.ErrCBTNotEnabled) {
			return err
		}
		snapshotChangeId = &vmware.ChangeID{}
	}

	needFullCopy, targetIsClean, err := target.NeedsFullCopy(ctx, t)
	if err != nil {
		return err
	}

	err = t.Connect(ctx)
	if err != nil {
		return err
	}
	defer t.Disconnect(ctx)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Warn("Received interrupt signal, cleaning up...")

		err := t.Disconnect(ctx)
		if err != nil {
			log.WithError(err).Fatal("Failed to disconnect from target")
		}

		os.Exit(1)
	}()

	path, err := t.GetPath(ctx)
	if err != nil {
		return err
	}

	if needFullCopy {
		err = s.FullCopyToTarget(t, path, targetIsClean)
		if err != nil {
			return err
		}
	} else {
		err = s.IncrementalCopyToTarget(ctx, t, path)
		if err != nil {
			return err
		}
	}

	if runV2V {
		log.Info("Running virt-v2v-in-place")

		os.Setenv("LIBGUESTFS_BACKEND", "direct")

		var cmd *exec.Cmd
		if s.Servers.VddkConfig.Debug {
			cmd = exec.Command("virt-v2v-in-place", "-v", "-x", "-i", "disk", path)
		} else {
			cmd = exec.Command("virt-v2v-in-place", "-i", "disk", path)
		}

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Run()
		if err != nil {
			return err
		}

		err = t.WriteChangeID(ctx, &vmware.ChangeID{})
		if err != nil {
			return err
		}
	} else {
		err = t.WriteChangeID(ctx, snapshotChangeId)
		if err != nil {
			return err
		}
	}

	if fixNicNames {
		if err := s.injectUdevNicRules(ctx, path); err != nil {
			return err
		}
	}

	return nil
}

func (s *NbdkitServer) injectUdevNicRules(ctx context.Context, path string) error {
	if path == "" {
		log.Warning("No block device path available, skipping udev NIC rule injection")
		return nil
	}

	devices, err := s.Servers.VirtualMachine.Device(ctx)
	if err != nil {
		return err
	}

	nics := devices.SelectByType((*types.VirtualEthernetCard)(nil))
	if len(nics) == 0 {
		log.Info("No network adapters found, skipping udev NIC rule injection")
		return nil
	}

	var echoLines []string
	for i, nic := range nics {
		card := nic.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard()
		name := nicInterfaceName(nic, i)

		log.WithFields(log.Fields{
			"mac":  card.MacAddress,
			"name": name,
		}).Info("Adding udev NIC rule")

		rule := fmt.Sprintf(
			`SUBSYSTEM=="net", ACTION=="add", ATTR{address}=="%s", NAME="%s"`,
			card.MacAddress, name,
		)
		echoLines = append(echoLines, fmt.Sprintf("echo '%s'", rule))
	}

	// Write the rules file inline via --run-command to avoid inode corruption
	// that --upload can cause on ext4 filesystems.
	shellCmd := fmt.Sprintf(
		"{ %s; } > /etc/udev/rules.d/70-persistent-net.rules",
		strings.Join(echoLines, "; "),
	)

	os.Setenv("LIBGUESTFS_BACKEND", "direct")
	cmd := exec.Command("virt-customize", "-a", path, "--run-command", shellCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.WithField("path", path).Info("Injecting udev NIC rules into guest")
	return cmd.Run()
}

func nicInterfaceName(nic types.BaseVirtualDevice, index int) string {
	device := nic.GetVirtualDevice()

	if device.SlotInfo != nil {
		if pci, ok := device.SlotInfo.(*types.VirtualDevicePciBusSlotInfo); ok && pci.PciSlotNumber > 0 {
			return fmt.Sprintf("ens%d", pci.PciSlotNumber)
		}
	}

	// Fallback: VMXNET3 uses PCI slots 192, 224, 256, ... (192 + 32*index)
	if _, ok := nic.(*types.VirtualVmxnet3); ok {
		return fmt.Sprintf("ens%d", 192+32*index)
	}

	return fmt.Sprintf("eth%d", index)
}
