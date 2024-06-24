package main

import (
	"context"
	"errors"
	"net/url"
	"os"

	"github.com/erikgeiser/promptkit/confirmation"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/vexxhost/migratekit/cmd"
	"github.com/vexxhost/migratekit/internal/openstack"
	"github.com/vexxhost/migratekit/internal/vmware"
	"github.com/vexxhost/migratekit/internal/vmware_nbdkit"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var (
	endpoint       string
	username       string
	password       string
	path           string
	flavorId       string
	networkMapping cmd.NetworkMappingFlag
)

var rootCmd = &cobra.Command{
	Use:   "migratekit",
	Short: "Near-live migration toolkit for VMware to OpenStack",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		endpointUrl := &url.URL{
			Scheme: "https",
			Host:   endpoint,
			User:   url.UserPassword(username, password),
			Path:   "sdk",
		}

		var err error
		thumbprint, err := vmware.GetEndpointThumbprint(endpointUrl)
		if err != nil {
			return err
		}

		ctx := context.TODO()
		conn, err := govmomi.NewClient(ctx, endpointUrl, true)
		if err != nil {
			return err
		}

		finder := find.NewFinder(conn.Client)
		vm, err := finder.VirtualMachine(ctx, path)
		if err != nil {
			return err
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
			Endpoint:   endpointUrl,
			Thumbprint: thumbprint,
		})

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
		err = servers.MigrationCycle(ctx, true)
		if err != nil {
			return err
		}

		log.Info("Final migration cycle completed, spinning up new OpenStack VM")

		err = clients.CreateResourcesForVirtualMachine(ctx, vm, flavorId, networks)
		if err != nil {
			return err
		}

		log.Info("Cutover completed")

		return nil
	},
}

func init() {
	log.SetLevel(log.DebugLevel)

	rootCmd.PersistentFlags().StringVar(&endpoint, "vmware-endpoint", "", "VMware endpoint (hostname or IP only)")
	rootCmd.MarkPersistentFlagRequired("vmware-endpoint")

	rootCmd.PersistentFlags().StringVar(&username, "vmware-username", "", "VMware username")
	rootCmd.MarkPersistentFlagRequired("vmware-username")

	rootCmd.PersistentFlags().StringVar(&password, "vmware-password", "", "VMware password")
	rootCmd.MarkPersistentFlagRequired("vmware-password")

	rootCmd.PersistentFlags().StringVar(&path, "vmware-path", "", "VMware VM path (e.g. '/Datacenter/vm/VM')")
	rootCmd.MarkPersistentFlagRequired("vmware-path")

	cutoverCmd.Flags().StringVar(&flavorId, "flavor", "", "OpenStack Flavor ID")
	cutoverCmd.MarkFlagRequired("flavor")

	cutoverCmd.Flags().Var(&networkMapping, "network-mapping", "Network mapping (e.g. 'mac=00:11:22:33:44:55,network-id=6bafb3d3-9d4d-4df1-86bb-bb7403403d24,subnet-id=47ed1da7-82d4-4e67-9bdd-5cb4993e06ff[,ip=1.2.3.4]')")
	cutoverCmd.MarkFlagRequired("network-mapping")

	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(cutoverCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
