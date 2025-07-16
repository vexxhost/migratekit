package vmware_nbdkit

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/francoisovh/migratekit/internal/nbdcopy"
	"github.com/francoisovh/migratekit/internal/nbdkit"
	"github.com/francoisovh/migratekit/internal/progress"
	"github.com/francoisovh/migratekit/internal/target"
	"github.com/francoisovh/migratekit/internal/vmware"
	log "github.com/sirupsen/logrus"
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
	jobIDVal := ctx.Value("jobID")
	jobID, ok := jobIDVal.(string)
	if !ok {
		log.Warn("jobID missing or not a string in context")
		jobID = "unknown"
	}
	bar := progress.NewVMwareProgressBar(jobID, "Creating snapshot")
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
	jobIDVal := ctx.Value("jobID")
	jobID, ok := jobIDVal.(string)
	if !ok {
		log.Warn("jobID missing or not a string in context")
		jobID = "unknown"
	}
	bar := progress.NewVMwareProgressBar(jobID, "Removing snapshot")
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

	for index, server := range s.Servers {
		t, err := target.NewOpenStack(ctx, s.VirtualMachine, server.Disk)
		if err != nil {
			return err
		}

		if index != 0 {
			runV2V = false
		}

		err = server.SyncToTarget(ctx, t, runV2V)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *NbdkitServer) FullCopyToTarget(t target.Target, path string, targetIsClean bool, ctx context.Context) error {
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
		ctx,
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

func (s *NbdkitServer) SyncToTarget(ctx context.Context, t target.Target, runV2V bool) error {
	snapshotChangeId, err := vmware.GetChangeID(s.Disk)
	if err != nil {
		return err
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
		err = s.FullCopyToTarget(t, path, targetIsClean, ctx)
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
			cmd = exec.Command("virt-v2v-in-place", "-v", "-x", "--no-selinux-relabel", "-i", "disk", path)
		} else {
			cmd = exec.Command("virt-v2v-in-place", "--no-selinux-relabel", "-i", "disk", path)
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

	return nil
}
