package openstack

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	log "github.com/sirupsen/logrus"
	"github.com/vexxhost/migratekit/cmd"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var ErrorVolumeNotFound = errors.New("volume not found")

type ClientSet struct {
	BlockStorage *gophercloud.ServiceClient
	Compute      *gophercloud.ServiceClient
	Networking   *gophercloud.ServiceClient
}

type PortCreateOpts struct {
	SecurityGroups *[]string
}

func NewClientSet(ctx context.Context) (*ClientSet, error) {
	opts, err := openstack.AuthOptionsFromEnv()
	if err != nil {
		return nil, err
	}

	provider, err := openstack.NewClient(opts.IdentityEndpoint)
	if err != nil {
		return nil, err
	}

	ua := gophercloud.UserAgent{}
	ua.Prepend("migratekit")
	provider.UserAgent = ua

	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if os.Getenv("OS_INSECURE") == "true" {
		config.InsecureSkipVerify = true
	}

	provider.HTTPClient.Transport = &http.Transport{
		TLSClientConfig: config,
	}

	err = openstack.Authenticate(ctx, provider, opts)
	if err != nil {
		return nil, err
	}

	blockStorageClient, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	})
	if err != nil {
		return nil, err
	}

	computeClient, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	})
	if err != nil {
		return nil, err
	}

	networkingClient, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	})
	if err != nil {
		return nil, err
	}

	return &ClientSet{
		BlockStorage: blockStorageClient,
		Compute:      computeClient,
		Networking:   networkingClient,
	}, nil
}

func (c *ClientSet) GetVolumeForDisk(ctx context.Context, vm *object.VirtualMachine, disk *types.VirtualDisk) (*volumes.Volume, error) {

	vzUnsafeVolumeByName := ctx.Value("vzUnsafeVolumeByName").(bool)

	volumsListOpts := volumes.ListOpts{
		Name: VolumeName(vm, disk),
	}

	if !vzUnsafeVolumeByName {
		volumsListOpts.Metadata = map[string]string{
			"migrate_kit": "true",
			"vm":          vm.Reference().Value,
			"disk":        strconv.Itoa(int(disk.Key)),
		}
	}

	pages, err := volumes.List(c.BlockStorage, volumsListOpts).AllPages(ctx)
	if err != nil {
		return nil, err
	}

	volumeList, err := volumes.ExtractVolumes(pages)
	if err != nil {
		return nil, err
	}

	// Deprecated, ensuring backward compatibility
	// TODO: remove
	if len(volumeList) == 0 {
		volumeList, err = c.GetVolumeListForDiskOld(ctx, vm, disk)
		if err != nil {
			return nil, err
		}
		if len(volumeList) > 0 {
			log.Warn("Using deprecated volume name and metadata format")
		}
	}

	if len(volumeList) == 0 {
		return nil, ErrorVolumeNotFound
	} else if len(volumeList) > 1 {
		return nil, errors.New("multiple volumes found")
	}

	return volumes.Get(ctx, c.BlockStorage, volumeList[0].ID).Extract()
}

// Deprecated, ensuring backward compatibility
// TODO: remove
func (c *ClientSet) GetVolumeListForDiskOld(ctx context.Context, vm *object.VirtualMachine, disk *types.VirtualDisk) ([]volumes.Volume, error) {
	pages, err := volumes.List(c.BlockStorage, volumes.ListOpts{
		Name: VolumeNameOld(vm, disk),
		Metadata: map[string]string{
			"migrate_kit": "true",
			"vm":          vm.Reference().Value,
			"disk":        disk.DiskObjectId,
		},
	}).AllPages(ctx)
	if err != nil {
		return nil, err
	}

	volumeList, err := volumes.ExtractVolumes(pages)

	return volumeList, err
}

func (c *ClientSet) EnsurePortsForVirtualMachine(ctx context.Context, vm *object.VirtualMachine, networkMappings *cmd.NetworkMappingFlag) ([]servers.Network, error) {
	devices, err := vm.Device(context.Background())
	if err != nil {
		return nil, err
	}

	var networks []servers.Network
	nics := devices.SelectByType((*types.VirtualEthernetCard)(nil))

	for _, nic := range nics {
		card := nic.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard()

		mapping, ok := networkMappings.Mappings[card.MacAddress]
		if !ok {
			return nil, errors.New("no network mapping found for MAC address")
		}

		pages, err := ports.List(c.Networking, ports.ListOpts{
			NetworkID:  mapping.NetworkID.String(),
			MACAddress: card.MacAddress,
		}).AllPages(ctx)
		if err != nil {
			return nil, err
		}

		portList, err := ports.ExtractPorts(pages)
		if err != nil {
			return nil, err
		}

		var port *ports.Port
		if len(portList) == 0 {
			var ips []ports.IP
			if mapping.IPAddress == nil {
				ips = []ports.IP{
					{
						SubnetID: mapping.SubnetID.String(),
					},
				}
			} else {
				ips = []ports.IP{
					{
						SubnetID:  mapping.SubnetID.String(),
						IPAddress: mapping.IPAddress.String(),
					},
				}
			}

			opts := ctx.Value("portCreateOpts").(*PortCreateOpts)
			port, err = ports.Create(ctx, c.Networking, ports.CreateOpts{
				NetworkID:      mapping.NetworkID.String(),
				Name:           card.DeviceInfo.GetDescription().Label,
				Description:    card.DeviceInfo.GetDescription().Summary,
				MACAddress:     card.MacAddress,
				FixedIPs:       ips,
				SecurityGroups: opts.SecurityGroups,
			}).Extract()
			if err != nil {
				return nil, err
			}

			log.WithFields(log.Fields{
				"port": port.ID,
			}).Info("Port created")
		} else if len(portList) == 1 {
			port = &portList[0]

			log.WithFields(log.Fields{
				"port": port.ID,
			}).Info("Port already exists")
		} else {
			return nil, errors.New("multiple ports found")
		}

		networks = append(networks, servers.Network{
			Port: port.ID,
		})
	}

	return networks, nil
}

func (c *ClientSet) CreateResourcesForVirtualMachine(ctx context.Context, vm *object.VirtualMachine, flavor string, networks []servers.Network, availabilityZone string) error {
	var o mo.VirtualMachine
	err := vm.Properties(ctx, vm.Reference(), []string{"config"}, &o)
	if err != nil {
		return err
	}

	devices, err := vm.Device(context.Background())
	if err != nil {
		return err
	}

	var blockDevices []servers.BlockDevice
	disks := devices.SelectByType((*types.VirtualDisk)(nil))
	diskIndex := 0
	for _, disk := range disks {
		vd := disk.(*types.VirtualDisk)
		volume, err := c.GetVolumeForDisk(ctx, vm, vd)
		if err != nil {
			return err
		}

		blockDevices = append(blockDevices, servers.BlockDevice{
			BootIndex:       diskIndex,
			SourceType:      servers.SourceVolume,
			UUID:            volume.ID,
			DestinationType: servers.DestinationVolume,
		})
		diskIndex++
	}

	server, err := servers.Create(ctx, c.Compute, servers.CreateOpts{
		Name:             o.Config.Name,
		FlavorRef:        flavor,
		Networks:         networks,
		BlockDevice:      blockDevices,
		AvailabilityZone: availabilityZone,
	}, servers.SchedulerHintOpts{}).Extract()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	err = servers.WaitForStatus(ctx, c.Compute, server.ID, "ACTIVE")
	if err != nil {
		return err
	}

	return nil
}
