package target

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
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

type VolumeCreateOpts struct {
	AvailabilityZone string
	VolumeType       string
	BusType          string
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
	volumeMetadata := map[string]string{
		"migrate_kit": "true",
		"vm":          t.VirtualMachine.Reference().Value,
		"disk":        strconv.Itoa(int(t.Disk.Key)),
	}

	opts := ctx.Value("volumeCreateOpts").(*VolumeCreateOpts)

	if opts.BusType == "scsi" {
		volumeMetadata["hw_disk_bus"] = "scsi"
		volumeMetadata["hw_scsi_model"] = "virtio-scsi"
	}

	if errors.Is(err, openstack.ErrorVolumeNotFound) {
		log.Info("Creating new volume")
		volume, err = t.createVolume(ctx, opts, volumeMetadata)
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
			err = t.VirtualMachine.Properties(ctx, t.VirtualMachine.Reference(), []string{"config.firmware", "config.guestId", "config.guestFullName"}, &o)
			if err != nil {
				return err
			}
			log.WithFields(log.Fields{
				"Config.GuestId":       o.Config.GuestId,
				"Config.GuestFullName": o.Config.GuestFullName,
			}).Info("VMware GustId")

			volumeImageMetadata := map[string]string{}
            switch osTypeCMD := ctx.Value("osType").(string); osTypeCMD{
                case "auto":
                    guestIdLower := strings.ToLower(o.Config.GuestId)
                    vmOsType := "linux" // linux is the default os type, TODO: Add mapping for all possible GuestIds
                    if strings.Contains(guestIdLower, "windows") {
                        vmOsType = "windows"
                    }
                    volumeImageMetadata["os_type"] = vmOsType
                case "":
                default:
                    volumeImageMetadata["os_type"] = osTypeCMD

            }

            if osType, ok := volumeImageMetadata["os_type"]; ok {
                log.WithFields(log.Fields{
                    "volume_id": volume.ID,
                    "os_type":   osType,
                }).Info("Volume set os type")
            }

			if types.GuestOsDescriptorFirmwareType(o.Config.Firmware) == types.GuestOsDescriptorFirmwareTypeEfi {
				log.WithFields(log.Fields{
					"volume_id": volume.ID,
				}).Info("Setting volume to be UEFI")
				volumeImageMetadata["hw_machine_type"] = "q35"
				volumeImageMetadata["hw_firmware_type"] = "uefi"
			}
			err = volumes.SetImageMetadata(ctx, t.ClientSet.BlockStorage, volume.ID, volumes.ImageMetadataOpts{
				Metadata: volumeImageMetadata,
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

func (t *OpenStack) createVolume(ctx context.Context, opts *VolumeCreateOpts, metadata map[string]string) (*volumes.Volume, error) {
	log.Info("Creating new volume")
	volume, err := volumes.Create(ctx, t.ClientSet.BlockStorage, volumes.CreateOpts{
		Name:             DiskLabel(t.VirtualMachine, t.Disk),
		Size:             int(math.Ceil(float64(t.Disk.CapacityInBytes) / 1024 / 1024 / 1024)),
		AvailabilityZone: opts.AvailabilityZone,
		VolumeType:       opts.VolumeType,
		Metadata:         metadata,
	}, nil).Extract()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	err = volumes.WaitForStatus(ctx, t.ClientSet.BlockStorage, volume.ID, "available")
	if err != nil {
		return nil, errors.Join(errors.New("timed out waiting for volume to be available"), err)
	}

	return volume, nil
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

		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		err = volumes.WaitForStatus(ctx, t.ClientSet.BlockStorage, volume.ID, "available")
		if err != nil {
			return errors.Join(errors.New("timed out waiting for volume to be available"), err)
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

	volume.Metadata["change_id"] = changeID.Value

	_, err = volumes.Update(ctx, t.ClientSet.BlockStorage, volume.ID, volumes.UpdateOpts{
		Metadata: volume.Metadata,
	}).Extract()

	return err
}
