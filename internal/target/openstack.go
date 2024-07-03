package target

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	log "github.com/sirupsen/logrus"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vexxhost/migratekit/internal/vmware"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type OpenStack struct {
	VirtualMachine *object.VirtualMachine
	Disk           *types.VirtualDisk
	ClientSet      *openstack.ClientSet
}

func NewOpenStack(ctx context.Context, vm *object.VirtualMachine, disk *types.VirtualDisk) (*OpenStack, error) {
	clientSet, err := openstack.NewClientSet(ctx)
	if err != nil {
		return nil, err
	}

	return &OpenStack{
		VirtualMachine: vm,
		Disk:           disk,
		ClientSet:      clientSet,
	}, nil
}

func (t *OpenStack) GetDisk() *types.VirtualDisk {
	return t.Disk
}

func findDevice(volumeID string) (string, error) {
	files, err := os.ReadDir("/dev/disk/by-id/")
	if err != nil {
		return "", err
	}

	for _, file := range files {
		if strings.Contains(file.Name(), volumeID[:18]) {
			devicePath, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-id/", file.Name()))
			if err != nil {
				return "", err
			}

			return devicePath, nil
		}
	}

	return "", nil
}

func (t *OpenStack) Connect(ctx context.Context) error {
	volume, err := t.ClientSet.GetVolumeForDisk(ctx, t.VirtualMachine, t.Disk)
	if errors.Is(err, openstack.ErrorVolumeNotFound) {
		log.Info("Creating new volume")
		volume, err = volumes.Create(ctx, t.ClientSet.BlockStorage, volumes.CreateOpts{
			Name: DiskLabel(t.VirtualMachine, t.Disk),
			Size: int(t.Disk.CapacityInBytes) / 1024 / 1024 / 1024,
			// FIXME: not only should this be optional, but it should be a variable but
			// danny and michael are special and can't figure out how to pass variables
			// to this function
			// AvailabilityZone: "availabilityZone",
			// VolumeType:       volumeType,
			AvailabilityZone: "gb-lon-3",
			VolumeType:       "st-retail-devtest-1000",
			Metadata: map[string]string{
				"migrate_kit": "true",
				"vm":          t.VirtualMachine.Reference().Value,
				"disk":        t.Disk.DiskObjectId,
			},
		}, nil).Extract()
		if err != nil {
			return err
		}

		// TODO: check if volume is bootable?
		if true {
			log.WithFields(log.Fields{
				"volume_id": volume.ID,
			}).Info("Volume created, setting to bootable")
			err := volumes.SetBootable(ctx, t.ClientSet.BlockStorage, volume.ID, volumes.BootableOpts{
				Bootable: true,
			}).ExtractErr()
			if err != nil {
				return err
			}

			var o mo.VirtualMachine
			err = t.VirtualMachine.Properties(ctx, t.VirtualMachine.Reference(), []string{"config.firmware"}, &o)
			if err != nil {
				return err
			}

			if types.GuestOsDescriptorFirmwareType(o.Config.Firmware) == types.GuestOsDescriptorFirmwareTypeEfi {
				log.WithFields(log.Fields{
					"volume_id": volume.ID,
				}).Info("Setting volume to be UEFI")
				err := volumes.SetImageMetadata(ctx, t.ClientSet.BlockStorage, volume.ID, volumes.ImageMetadataOpts{
					Metadata: map[string]string{
						"hw_machine_type":  "q35",
						"hw_firmware_type": "uefi",
					},
				}).ExtractErr()
				if err != nil {
					return err
				}
			}
			// FIXME: review this and see if we can be combined with a block above
			log.WithFields(log.Fields{
				"volume_id": volume.ID,
			}).Info("Setting volume to be SCSI")
			err = volumes.SetImageMetadata(ctx, t.ClientSet.BlockStorage, volume.ID, volumes.ImageMetadataOpts{
				Metadata: map[string]string{
					"hw_disk_bus":   "scsi",
					"hw_scsi_model": "virtio-scsi",
				},
			}).ExtractErr()
			if err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"volume_id": volume.ID,
	}).Info("Attaching volume")

	path, err := t.GetPath(ctx)
	if err != nil {
		return err
	}

	if path == "" {
		instanceUUID, err := openstack.GetCurrentInstanceUUID()
		if err != nil {
			return err
		}

		log.WithFields(log.Fields{
			"instance_uuid": instanceUUID,
		}).Info("Detected instance UUID, attaching volume...")

		_, err = volumeattach.Create(ctx, t.ClientSet.Compute, instanceUUID, volumeattach.CreateOpts{
			VolumeID: volume.ID,
		}).Extract()
		if err != nil {
			return err
		}

		timeoutTimer := time.After(2 * time.Minute)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-timeoutTimer:
				return errors.New("timed out waiting for volume to attach")
			case <-ticker.C:
				devicePath, err := findDevice(volume.ID)
				if err != nil {
					return err
				}

				if devicePath != "" {
					log.WithFields(log.Fields{
						"volume_id": volume.ID,
						"device":    devicePath,
					}).Info("Device found")

					return nil
				}

				log.WithFields(log.Fields{
					"volume_id": volume.ID,
				}).Info("Device for volume not found, checking again...")
			}
		}
	}

	return nil
}

func (t *OpenStack) GetPath(ctx context.Context) (string, error) {
	volume, err := t.ClientSet.GetVolumeForDisk(ctx, t.VirtualMachine, t.Disk)
	if err != nil {
		return "", err
	}

	devicePath, err := findDevice(volume.ID)
	if err != nil {
		return "", err
	}

	return devicePath, nil
}

func (t *OpenStack) Disconnect(ctx context.Context) error {
	volume, err := t.ClientSet.GetVolumeForDisk(ctx, t.VirtualMachine, t.Disk)
	if errors.Is(err, openstack.ErrorVolumeNotFound) {
		return nil
	} else if err != nil {
		return err
	}

	devicePath, err := findDevice(volume.ID)
	if err != nil {
		return err
	}

	if devicePath != "" {
		instanceUUID, err := openstack.GetCurrentInstanceUUID()
		if err != nil {
			return err
		}

		err = volumeattach.Delete(ctx, t.ClientSet.Compute, instanceUUID, volume.ID).ExtractErr()
		if err != nil {
			return err
		}
	}

	return nil
}

func (t *OpenStack) Exists(ctx context.Context) (bool, error) {
	_, err := t.ClientSet.GetVolumeForDisk(ctx, t.VirtualMachine, t.Disk)
	if errors.Is(err, openstack.ErrorVolumeNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}

func (t *OpenStack) GetCurrentChangeID(ctx context.Context) (*vmware.ChangeID, error) {
	volume, err := t.ClientSet.GetVolumeForDisk(ctx, t.VirtualMachine, t.Disk)
	if errors.Is(err, openstack.ErrorVolumeNotFound) {
		return &vmware.ChangeID{}, nil
	} else if err != nil {
		return nil, err
	}

	if changeID, ok := volume.Metadata["change_id"]; ok {
		return vmware.ParseChangeID(changeID)
	}

	return &vmware.ChangeID{}, nil
}

func (t *OpenStack) WriteChangeID(ctx context.Context, changeID *vmware.ChangeID) error {
	volume, err := t.ClientSet.GetVolumeForDisk(ctx, t.VirtualMachine, t.Disk)
	if errors.Is(err, openstack.ErrorVolumeNotFound) {
		return nil
	}

	_, err = volumes.Update(ctx, t.ClientSet.BlockStorage, volume.ID, volumes.UpdateOpts{
		Metadata: map[string]string{
			"migrate_kit": "true",
			"vm":          t.VirtualMachine.Reference().Value,
			"disk":        t.Disk.DiskObjectId,
			"change_id":   changeID.Value,
		},
	}).Extract()

	return err
}
