# migratekit

Migratekit is a CLI tool which can help you to migrate your virtual machines
from VMware to OpenStack in a near-zero downtime.  The goal of this project
is to allow you to move as much data as possible online and only have a short
downtime window to complete the migration.

## How it works

Migratekit has two phases that it operates in to allow you to migrate your
virtual machine with almost no downtime which are documented below.

These phases allow you to move data online as much as possible and have a 
final cutover phase which takes the virtual machine offline for a short
period of time to complete the migration.

### Migration phase

You will generally run as many migration cycles as you need, they will run
without any downtime to the virtual machine.

On your first migration cycle, Migratekit will do a full copy of the virtual
machine to the OpenStack cloud.  On subsequent migration cycles, Migratekit
will only copy the changes that have been made to the virtual machine since the
last migration cycle.

### Cutover phase

Once you are ready to cut over to the OpenStack cloud, you will run the cutover
phase.  This will ensure all of the matching resources such as Neutron ports
exist on the cloud, do a final sync of the virtual machine, power it off inside
of VMware, execute a final sync, and then build a new virtual machine on the
OpenStack cloud using the same volumes as the original virtual machine.

## Usage

The easiest supported way of running Migratekit is by using the Docker image
which is shipped with all of the dependencies required to run Migratekit except
for the [VMware Disk Development Kit (VDDK)](https://developer.broadcom.com/sdks/vmware-virtual-disk-development-kit-vddk/8.0) which you will need to download
from the VMware website.

> [!NOTE]
> At the moment, Migratekit only supports VMware hypervisors that expose an API
> endpoint.  If you see the following error message, it means that Migratekit
> does not support your VMware hypervisor:
>
> ```
> Error: ServerFaultCode: Current license or ESXi version prohibits execution of the requested operation.
> ```

### Installing VDDK

In order to be able to use Migratekit, you will need to download the VDDK from
the VMware website.  You will ened to create a developer account in order to
download the VDDK as well as accept the EULA.

1. Download [VMware Virtual Disk Development Kit (VDDK) 8.0.2 for Linux](https://developer.broadcom.com/sdks/vmware-virtual-disk-development-kit-vddk/8.0)
2. Once you've downloaded the file, you will need to extract the contents of the
   tarball to a directory on your system.
3. We recommend that you extract the contents of the tarball to `/usr/lib64/vmware-vix-disklib/`
   on your system.  If you decide to extract the contents to a different directory,
   you will need to update the Docker volume mount in the next section to reflect
   the directory you extracted the tarball to.

### Configuring account for Migratekit

If you are using vCenter, you will need an account with at least the following permissions
to be able to use Migratekit:

| Privilege Name in the vSphere Client             | Purpose                                                            | Required On                   | Privilege Name in the API                          |
|--------------------------------------------------|--------------------------------------------------------------------|-------------------------------|----------------------------------------------------|
| Browse datastore                                 | Enable browsing of VM log files for troubleshooting snapshot issues. | Data stores                   | Datastore.Browse                                   |
| Low level file operations                        | Permit read/write/delete/rename operations in the datastore browser for snapshot troubleshooting. | Data stores                   | Datastore.FileManagement                           |
| Change Configuration - Toggle disk change tracking | Enable or disable change tracking of VM disks to manage data changes between snapshots. | Virtual machines               | VirtualMachine.Config.ChangeTracking               |
| Change Configuration - Acquire disk lease        | Allow disk lease operations to read VM disks using the VMware vSphere Virtual Disk Development Kit (VDDK). | Virtual machines               | VirtualMachine.Config.DiskLease                    |
| Provisioning - Allow read-only disk access       | Enable read-only access to VM disks for data reading using the VDDK. | Virtual machines               | VirtualMachine.Provisioning.DiskRandomRead         |
| Provisioning - Allow disk access                 | Permit read access to VM disks for troubleshooting using the VDDK.  | Virtual machines               | VirtualMachine.Provisioning.DiskRandomAccess       |
| Provisioning - Allow virtual machine download    | Enable downloading of VM-related files to facilitate troubleshooting. | Root host or vCenter Server    | VirtualMachine.Provisioning.GetVmFiles             |
| Snapshot management                              | Facilitate Discovery, Software Inventory, and Dependency Mapping on VMs. | Virtual machines               | VirtualMachine.State.*                             |
| Guest operations                                 | Allow creation and management of VM snapshots for replication.      | Virtual machines               | VirtualMachine.GuestOperations.*                   |
| Interaction Power Off                            | Permit powering off the VM during migration to Migratekit.         | Virtual machines               | VirtualMachine.Interact.PowerOff                   |

### Running Migratekit

Assuming that you already have Docker installed on your system, you can use the
following command to run the Migratekit Docker container:

> [!WARNING]
> Ubuntu Noble Numbat (24.04) LTS was released with additional restrictions on [unprivileged user namespaces](https://discourse.ubuntu.com/t/ubuntu-24-04-lts-noble-numbat-release-notes/39890#unprivileged-user-namespace-restrictions),
> we suggest either disabling them or using Ubuntu Jammy Jellyfish (22.04) LTS until a solution is found.

```bash
docker run -it --rm --privileged \
  --network host \
  -v /dev:/dev \
  -v /usr/lib64/vmware-vix-disklib/:/usr/lib64/vmware-vix-disklib:ro \
  --env-file <(env | grep OS_) \
  registry.atmosphere.dev/library/migratekit:latest \
  --help
```

If you want to get started with running your first migration cycle, you can run
it with the following command:

```bash
docker run -it --rm --privileged \
  --network host \
  -v /dev:/dev \
  -v /usr/lib64/vmware-vix-disklib/:/usr/lib64/vmware-vix-disklib:ro \
  --env-file <(env | grep OS_) \
  registry.atmosphere.dev/library/migratekit:latest \
  migrate \
  --vmware-endpoint vmware.local \
  --vmware-username username \
  --vmware-password password \
  --vmware-path /ha-datacenter/vm/migration-test
```

In the example above, you would run the migration cycle against a virtual machine
located at `/ha-datacenter/vm/migration-test` on the VMware endpoint `vmware.local`
(which can be both an ESXi host or a vCenter server).  The endpoint can also be
an IP address if you do not have a DNS entry for the endpoint.

You will also need to make sure you have all of your OpenStack environment variables
set in your environment before running the command so that Migratekit can connect
to the OpenStack cloud.

Once you've ran this command a few times and you're happy that you're ready to
cutover, you can run the following command to cutover to the OpenStack cloud:

```bash
docker run -it --rm --privileged \
  --network host \
  -v /dev:/dev \
  -v /usr/lib64/vmware-vix-disklib/:/usr/lib64/vmware-vix-disklib:ro \
  --env-file <(env | grep OS_) \
  registry.atmosphere.dev/library/migratekit:latest \
  cutover \
  --vmware-endpoint vmware.local \
  --vmware-username username \
  --vmware-password password \
  --vmware-path /ha-datacenter/vm/migration-test \
  --flavor b542cedb-d3b4-4446-a43f-5416711440ee \
  --network-mapping mac=00:0c:29:7d:2d:68,network-id=2a81f1b0-c1b8-48dd-bd8e-4d976608c06d,subnet-id=21a7110b-2ab2-4cc1-8372-8b552f7a4438,ip=192.168.2.20
```

You should run it with the same arguments, however, this time you will need to
specify the flavor that you want to use for the virtual machine on the OpenStack
cloud as well as the network mapping that you want to use for the virtual machine.

The network mapping allows Migratekit to know what network to attach the virtual
machine to on the OpenStack cloud, as well as the IP address that you want to
assign to the virtual machine since it cannot map this information from VMware.

The format of the network mapping is as follows:

- `mac`: The MAC address of the virtual interface on the virtual machine that you
         want to map to the OpenStack network (required).
- `network-id`: The UUID of the network that you want to attach the virtual machine
               to on the OpenStack cloud (required).
- `subnet-id`: The UUID of the subnet that you want to attach the virtual machine
               to on the OpenStack cloud (required).
- `ip`: The IP address that you want to assign to the virtual machine on the OpenStack
        cloud (optional, Neutron will assign an IP address if this is not specified).

You should ideally match the network mapping to the network that the virtual machine
is attached to on the VMware side to ensure that the virtual machine can communicate
with the network once it has been migrated to the OpenStack cloud.

You can use more than one network mapping in case your VMWare machine has more than one.

There are a few optional flags to define the following:
-  `--security-groups`: A comma separated list of security group UUIDs to apply
                       to the virtual machines port, if not supplied only the
                       'default' security group will be applied
-  `--volume-type`: Openstack volume type to be used for the block devices
-  `--availability-zone`: Opentack availabiity zone to be associated with both
                        block device and virtual machine
-  `--run-v2v`: A flag to disable the running of virt-v2v-in-place against the
              destination machine.  Should be disabled with caution as it may
              result in an unbootable instance. To disable flag must be passed
              with an =false e.g. `--run-v2v=false`
   `--disk-bus-type`: Flag to define volume disk bus type, currently only supports
                     scsi and virtio.
-   `--compression-method`: Compression method: skipz, zlib and none.
-   `--os-type`: Sets the "os_type" volume (image) metadata variable.
                 If set to "auto", it tries to recognize the correct operating
                 system via the VMware GuestId.
                 Valid values for the most OpenStack installations are "linux"
                 and "windows"
-   `--enable-qemu-guest-agent`: Sets the "hw_qemu_guest_agent" volume (image) metadata parameter to "yes".

## Contributing

We welcome contributions to this project, we hope to see this project grow and
become a useful tool for people to migrate their virtual machines from VMware
to OpenStack.

Since VMware environments can be quite complex, we are happy to work with
contributors to add support for more complex VMware environments to Migratekit
for the benefit of the community, but we need your help to do so because of
a lack of access to these environments and their licensing costs.

### Development

In order to have a development environment for Migratekit, it's recommended to
run your development inside of a Docker container.  You can use the following
command to run a development environment for Migratekit:

```bash
docker run -it --rm --privileged \
  --network host \
  -v /dev:/dev \
  -v /usr/lib64/vmware-vix-disklib/:/usr/lib64/vmware-vix-disklib:ro \
  -v $(pwd):/app \
  --entrypoint /bin/bash \
  fedora:40
```

Once you've launched this, it's recommended to add all of the development headers
and other components, so you can run the following:

```bash
dnf install nbdkit nbdkit-vddk-plugin libnbd golang libnbd-devel virt-v2v
```

From there, you'll be good to go by switching to the `/app` directory and running
any of the commands that you need to run for development.

## Support

If you need help with using Migratekit, you can either open an issue to get
help from the community or you can contact [VEXXHOST](https://vexxhost.com/contact-us/)
for professional support with VMware migration or OpenStack.
