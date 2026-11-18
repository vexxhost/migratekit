package vmware_nbdkit

import (
	"context"
	"errors"
	"net/url"
	"os"
	"syscall"
	"time"

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

type migrationTarget struct {
	Server        *NbdkitServer
	Target        target.Target
	Path          string
	NeedFullCopy  bool
	TargetIsClean bool
}

func (mt *migrationTarget) connect(ctx context.Context) error {
	var err error
	// Capture freshness before Connect can create the destination volume.
	mt.NeedFullCopy, mt.TargetIsClean, err = target.NeedsFullCopy(ctx, mt.Target)
	if err != nil {
		return err
	}
	if err := mt.Target.Connect(ctx); err != nil {
		return err
	}
	mt.Path, err = mt.Target.GetPath(ctx)
	return err
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
	var errs []error
	for _, server := range s.Servers {
		if err := server.Nbdkit.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.SnapshotRef.Value != "" {
		if err := s.removeSnapshot(ctx); err != nil {
			errs = append(errs, err)
		} else {
			s.SnapshotRef = types.ManagedObjectReference{}
		}
	}
	return errors.Join(errs...)
}

func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
}

func (s *NbdkitServers) MigrationCycle(ctx context.Context, runV2V bool) (err error) {
	defer func() {
		cleanup, cancel := cleanupContext(ctx)
		defer cancel()
		err = errors.Join(err, s.Stop(cleanup))
	}()
	if err := s.Start(ctx); err != nil {
		return err
	}
	// Closing the source connection releases a synchronous incremental read.
	// Volumes and snapshots are cleaned up by the owning goroutine afterward.
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		for _, server := range s.Servers {
			if err := server.Nbdkit.Interrupt(); err != nil {
				log.WithError(err).Error("Failed to interrupt source reader")
			}
		}
	})
	defer func() {
		if !stop() {
			<-done
		}
	}()
	if runV2V {
		return s.migrationCycleWithV2V(ctx)
	}
	for _, server := range s.Servers {
		t, err := target.NewOpenStack(ctx, s.VirtualMachine, server.Disk)
		if err != nil {
			return err
		}
		if err := server.SyncToTarget(ctx, t); err != nil {
			return err
		}
	}
	return nil
}

func disconnectMigrationTargets(ctx context.Context, targets []*migrationTarget) error {
	var errs []error

	for _, mt := range targets {
		if mt.Target == nil {
			continue
		}

		if err := mt.Target.Disconnect(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (s *NbdkitServers) migrationCycleWithV2V(ctx context.Context) (err error) {
	targets := []*migrationTarget{}
	defer func() {
		cleanup, cancel := cleanupContext(ctx)
		defer cancel()
		err = errors.Join(err, disconnectMigrationTargets(cleanup, targets))
	}()

	for _, server := range s.Servers {
		t, err := target.NewOpenStack(ctx, s.VirtualMachine, server.Disk)
		if err != nil {
			return err
		}

		mt := &migrationTarget{
			Server: server,
			Target: t,
		}
		targets = append(targets, mt)

		if err := mt.connect(ctx); err != nil {
			return err
		}
	}

	for _, mt := range targets {
		_, err := mt.Server.CopyToTarget(ctx, mt.Target, mt.Path, mt.NeedFullCopy, mt.TargetIsClean)
		if err != nil {
			return err
		}
	}

	return s.RunVirtV2VInPlace(ctx, targets)
}

func (s *NbdkitServer) FullCopyToTarget(ctx context.Context, t target.Target, path string, targetIsClean bool) error {
	logger := log.WithFields(log.Fields{
		"vm":   s.Servers.VirtualMachine.Name(),
		"disk": s.Disk.Backing.(types.BaseVirtualDeviceFileBackingInfo).GetVirtualDeviceFileBackingInfo().FileName,
	})

	logger.Info("Starting full copy")

	err := nbdcopy.Run(ctx,
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

	defer handle.Close()

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
				if err := ctx.Err(); err != nil {
					return err
				}
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

func (s *NbdkitServer) CopyToTarget(ctx context.Context, t target.Target, path string, needFullCopy, targetIsClean bool) (*vmware.ChangeID, error) {
	snapshotChangeId, err := vmware.GetChangeID(s.Disk)
	if err != nil {
		return nil, err
	}

	if needFullCopy {
		err = s.FullCopyToTarget(ctx, t, path, targetIsClean)
		if err != nil {
			return nil, err
		}
	} else {
		err = s.IncrementalCopyToTarget(ctx, t, path)
		if err != nil {
			return nil, err
		}
	}

	return snapshotChangeId, nil
}

func (s *NbdkitServer) SyncToTarget(ctx context.Context, t target.Target) (err error) {
	mt := &migrationTarget{Server: s, Target: t}
	defer func() {
		cleanup, cancel := cleanupContext(ctx)
		defer cancel()
		err = errors.Join(err, t.Disconnect(cleanup))
	}()
	err = mt.connect(ctx)
	if err != nil {
		return err
	}

	snapshotChangeId, err := s.CopyToTarget(ctx, t, mt.Path, mt.NeedFullCopy, mt.TargetIsClean)
	if err != nil {
		return err
	}

	err = t.WriteChangeID(ctx, snapshotChangeId)
	if err != nil {
		return err
	}

	return nil
}
