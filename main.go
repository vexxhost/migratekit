package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"time"

	"github.com/erikgeiser/promptkit/confirmation"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/thediveo/enumflag/v2"
	"github.com/vexxhost/migratekit/cmd"
	"github.com/vexxhost/migratekit/internal/nbdkit"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vexxhost/migratekit/internal/target"
	"github.com/vexxhost/migratekit/internal/vmware"
	"github.com/vexxhost/migratekit/internal/vmware_nbdkit"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/session/keepalive"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

type BusTypeOpts enumflag.Flag

const (
	Virtio BusTypeOpts = iota
	Scsi
)

var BusTypeOptsIds = map[BusTypeOpts][]string{
	Virtio: {"virtio"},
	Scsi:   {"scsi"},
}

type CompressionMethodOpts enumflag.Flag

const (
	None CompressionMethodOpts = iota
	Zlib
	Fastlz
	Skipz
)

var CompressionMethodOptsIds = map[CompressionMethodOpts][]string{
	None:   {"none"},
	Zlib:   {"zlib"},
	Fastlz: {"fastlz"},
	Skipz:  {"skipz"},
}

var (
	debug                bool
	endpoint             string
	username             string
	password             string
	path                 string
	compressionMethod    CompressionMethodOpts = Skipz
	flavorId             string
	networkMapping       cmd.NetworkMappingFlag
	availabilityZone     string
	volumeType           string
	securityGroups       []string
	enablev2v            bool
	busType              BusTypeOpts
	vzUnsafeVolumeByName bool
	osType               string
)

var rootCmd = &cobra.Command{
	Use:   "migratekit",
	Short: "Near-live migration toolkit for VMware to OpenStack",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if debug {
			log.SetLevel(log.DebugLevel)
		}

		endpointUrl := &url.URL{
			Scheme: "https",
			Host:   endpoint,
			User:   url.UserPassword(username, password),
			Path:   "sdk",
		}

		var err error

		// validBuses := []string{"scsi", "virtio"}
		// if !slices.Contains(validBuses, busType) {
		// 	log.Fatal("Invalid bus type: ", busType, ". Valid options are: ", validBuses)
		// }

		thumbprint, err := vmware.GetEndpointThumbprint(endpointUrl)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		soapClient := soap.NewClient(endpointUrl, true)
		vimClient, err := vim25.NewClient(ctx, soapClient)
		if err != nil {
			log.WithError(err).Error("Failed to create VMware client")
			return err
		}

		vimClient.RoundTripper = keepalive.NewHandlerSOAP(
			vimClient.RoundTripper,
			15*time.Second,
			nil,
		)

		mgr := session.NewManager(vimClient)
		err = mgr.Login(ctx, endpointUrl.User)
		if err != nil {
			log.WithError(err).Error("Failed to login to VMware")
			return err
		}

		finder := find.NewFinder(vimClient)
		vm, err := finder.VirtualMachine(ctx, path)

		if err != nil {
			switch err.(type) {
			case *find.NotFoundError:
				log.WithError(err).Error("Virtual machine not found, list of all virtual machines:")

				vms, err := finder.VirtualMachineList(ctx, "*")
				if err != nil {
					return err
				}

				for _, vm := range vms {
					log.Info(" - ", vm.InventoryPath)
				}

				os.Exit(1)
			default:
				return err
			}
		}

		var o mo.VirtualMachine
		err = vm.Properties(ctx, vm.Reference(), []string{"config"}, &o)
		if err != nil {
			return err
		}

		if o.Config.ChangeTrackingEnabled == nil || !*o.Config.ChangeTrackingEnabled {
			return errors.New("change tracking is not enabled on the virtual machine")
		}

		if snapshotRef, _ := vm.FindSnapshot(ctx, "migratekit"); snapshotRef != nil {
			log.Info("Snapshot already exists")

			input := confirmation.New("Delete existing snapshot?", confirmation.Undecided)
			delete, err := input.RunPrompt()
			if err != nil {
				return err
			}

			if delete {
				consolidate := true
				_, err := vm.RemoveSnapshot(ctx, snapshotRef.Value, false, &consolidate)
				if err != nil {
					return err
				}
			} else {
				return errors.New("unable to continue without deleting existing snapshot")
			}
		}

		ctx = context.WithValue(ctx, "vm", vm)
		ctx = context.WithValue(ctx, "vddkConfig", &vmware_nbdkit.VddkConfig{
			Debug:       debug,
			Endpoint:    endpointUrl,
			Thumbprint:  thumbprint,
			Compression: nbdkit.CompressionMethod(CompressionMethodOptsIds[compressionMethod][0]),
		})

		log.Info("Setting Disk Bus: ", BusTypeOptsIds[busType][0])
		v := target.VolumeCreateOpts{
			AvailabilityZone: availabilityZone,
			VolumeType:       volumeType,
			BusType:          BusTypeOptsIds[busType][0],
		}
		ctx = context.WithValue(ctx, "volumeCreateOpts", &v)

		ctx = context.WithValue(ctx, "vzUnsafeVolumeByName", vzUnsafeVolumeByName)

		ctx = context.WithValue(ctx, "osType", osType)

		cmd.SetContext(ctx)

		return nil
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run a migration cycle",
	Long: `This command will run a migration cycle on the virtual machine without shutting off the source virtual machine.

- If no data for this virtual machine exists on the target, it will do a full copy.
- If data exists on the target, it will only copy the changed blocks.

It handles the following additional cases as well:

- If VMware indicates the change tracking has reset, it will do a full copy.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		vm := ctx.Value("vm").(*object.VirtualMachine)
		vddkConfig := ctx.Value("vddkConfig").(*vmware_nbdkit.VddkConfig)

		servers := vmware_nbdkit.NewNbdkitServers(vddkConfig, vm)
		err := servers.MigrationCycle(ctx, false)
		if err != nil {
			return err
		}

		log.Info("Migration completed")
		return nil
	},
}

var cutoverCmd = &cobra.Command{
	Use:   "cutover",
	Short: "Cutover to the new virtual machine",
	Long: `This commands will cutover into the OpenStack virtual machine from VMware by executing the following steps:

- Run a migration cycle
- Shut down the source virtual machine
- Run a final migration cycle to capture missing changes & run virt-v2v-in-place
- Spin up the new OpenStack virtual machine with the migrated disk`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		vm := ctx.Value("vm").(*object.VirtualMachine)
		vddkConfig := ctx.Value("vddkConfig").(*vmware_nbdkit.VddkConfig)

		clients, err := openstack.NewClientSet(ctx)
		if err != nil {
			return err
		}

		log.Info("Ensuring OpenStack resources exist")

		flavor, err := flavors.Get(ctx, clients.Compute, flavorId).Extract()
		if err != nil {
			return err
		}

		log.WithFields(log.Fields{
			"flavor": flavor.Name,
		}).Info("Flavor exists, ensuring network resources exist")

		v := openstack.PortCreateOpts{}
		if len(securityGroups) > 0 {
			v.SecurityGroups = &securityGroups
		}
		ctx = context.WithValue(ctx, "portCreateOpts", &v)

		networks, err := clients.EnsurePortsForVirtualMachine(ctx, vm, &networkMapping)
		if err != nil {
			return err
		}

		log.Info("Starting migration cycle")

		servers := vmware_nbdkit.NewNbdkitServers(vddkConfig, vm)
		err = servers.MigrationCycle(ctx, false)
		if err != nil {
			return err
		}

		log.Info("Completed migration cycle, shutting down source VM")

		powerState, err := vm.PowerState(ctx)
		if err != nil {
			return err
		}

		if powerState == types.VirtualMachinePowerStatePoweredOff {
			log.Warn("Source VM is already off, skipping shutdown")
		} else {
			err := vm.ShutdownGuest(ctx)
			if err != nil {
				return err
			}

			err = vm.WaitForPowerState(ctx, types.VirtualMachinePowerStatePoweredOff)
			if err != nil {
				return err
			}

			log.Info("Source VM shut down, starting final migration cycle")
		}

		servers = vmware_nbdkit.NewNbdkitServers(vddkConfig, vm)
		err = servers.MigrationCycle(ctx, enablev2v)
		if err != nil {
			return err
		}

		log.Info("Final migration cycle completed, spinning up new OpenStack VM")

		err = clients.CreateResourcesForVirtualMachine(ctx, vm, flavorId, networks, availabilityZone)
		if err != nil {
			return err
		}

		log.Info("Cutover completed")

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug logging")

	rootCmd.PersistentFlags().StringVar(&endpoint, "vmware-endpoint", "", "VMware endpoint (hostname or IP only)")
	rootCmd.MarkPersistentFlagRequired("vmware-endpoint")

	rootCmd.PersistentFlags().StringVar(&username, "vmware-username", "", "VMware username")
	rootCmd.MarkPersistentFlagRequired("vmware-username")

	rootCmd.PersistentFlags().StringVar(&password, "vmware-password", "", "VMware password")
	rootCmd.MarkPersistentFlagRequired("vmware-password")

	rootCmd.PersistentFlags().StringVar(&path, "vmware-path", "", "VMware VM path (e.g. '/Datacenter/vm/VM')")
	rootCmd.MarkPersistentFlagRequired("vmware-path")

	rootCmd.PersistentFlags().Var(enumflag.New(&compressionMethod, "compression-method", CompressionMethodOptsIds, enumflag.EnumCaseInsensitive), "compression-method", "Specifies the compression method to use for the disk")

	rootCmd.PersistentFlags().StringVar(&availabilityZone, "availability-zone", "", "Openstack availability zone for blockdevice & server")

	rootCmd.PersistentFlags().StringVar(&volumeType, "volume-type", "", "Openstack volume type")

	rootCmd.PersistentFlags().Var(enumflag.New(&busType, "disk-bus-type", BusTypeOptsIds, enumflag.EnumCaseInsensitive), "disk-bus-type", "Specifies the type of disk controller to attach disk devices to.")

	rootCmd.PersistentFlags().BoolVar(&vzUnsafeVolumeByName, "vz-unsafe-volume-by-name", false, "Only use the name to find a volume - workaround for virtuozzu - dangerous option")

    rootCmd.PersistentFlags().StringVar(&osType, "os-type", "", "Set os_type in the volume (image) metadata, (if set to \"auto\", it tries to detect the type from VMware GuestId)")

	cutoverCmd.Flags().StringVar(&flavorId, "flavor", "", "OpenStack Flavor ID")
	cutoverCmd.MarkFlagRequired("flavor")

	cutoverCmd.Flags().Var(&networkMapping, "network-mapping", "Network mapping (e.g. 'mac=00:11:22:33:44:55,network-id=6bafb3d3-9d4d-4df1-86bb-bb7403403d24,subnet-id=47ed1da7-82d4-4e67-9bdd-5cb4993e06ff[,ip=1.2.3.4]')")
	cutoverCmd.MarkFlagRequired("network-mapping")

	cutoverCmd.Flags().StringSliceVar(&securityGroups, "security-groups", nil, "Openstack security groups, comma separated (e.g. '42c5a89e-4034-4f2a-adea-b33adc9614f4,6647122c-2d46-42f1-bb26-f38007730fdc')")

	cutoverCmd.Flags().BoolVar(&enablev2v, "run-v2v", true, "Run virt2v-inplace on destination VM")

	cutoverCmd.Flags().StringVar(&availabilityZone, "availability-zone", "", "OpenStack availability zone for blockdevice & server")
	cutoverCmd.MarkFlagRequired("availability-zone")

	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(cutoverCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
