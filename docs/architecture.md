# Migratekit Architecture

This document is based on repository inspection. It should stay tied to the
actual package names, functions, and commands in this tree. If behavior is not
clear from the code, it is marked as unclear rather than inferred.

## Project Purpose

Migratekit is a CLI tool for near-live migration of VMware virtual machines to
OpenStack. The `README.md` describes two phases:

- `migrate`: run one or more online migration cycles while the VMware VM stays
  powered on.
- `cutover`: run a migration cycle, shut down the VMware VM, run a final cycle,
  optionally run `virt-v2v-in-place`, and boot an OpenStack server from the
  migrated volumes.

`AGENTS.md` describes this repository as a refined fork of VEXXHOST
`migratekit`, with a preference for upstream-compatible changes and migration
correctness over optimization.

## Repository Layout

- `main.go`: Cobra CLI definition, global flags, VMware session setup,
  `migrate` and `cutover` command flows.
- `cmd/flags.go`: custom `NetworkMappingFlag` parser for cutover network
  mappings.
- `internal/vmware/change_id.go`: VMware CBT change ID parsing and extraction
  from `types.VirtualDisk` backing structs.
- `internal/vmware/vddk.go`: endpoint TLS thumbprint discovery for VDDK.
- `internal/vmware_nbdkit/vmware_nbdkit.go`: snapshot lifecycle, per-disk
  `nbdkit` server lifecycle, full and incremental copy logic, and
  `virt-v2v-in-place` invocation.
- `internal/nbdkit/builder.go`: builds the external `nbdkit vddk` command line.
- `internal/nbdkit/server.go`: starts/stops the external `nbdkit` process and
  exposes the libnbd URI.
- `internal/nbdcopy/nbdcopy.go`: runs the external `nbdcopy` command for full
  copies.
- `internal/target/interface.go`: target abstraction used by the sync pipeline.
- `internal/target/openstack.go`: OpenStack target implementation for Cinder
  volumes attached to the current instance.
- `internal/target/util.go`: disk labels and full-copy versus incremental-copy
  decision logic.
- `internal/openstack/client.go`: Gophercloud client setup, volume lookup,
  Neutron port creation, and Nova server creation.
- `internal/openstack/util.go`: OpenStack metadata-service UUID lookup and
  volume naming helpers.
- `internal/progress/progress.go`: progress bar helpers for data copy and
  VMware task progress.
- `Dockerfile`: production/container image build, currently based on
  `fedora:44`.
- `hack/dev-shell.sh`: documented local development shell, currently based on
  `fedora:40`.
- `go.mod`: module definition, currently `github.com/vexxhost/migratekit` with
  `go 1.25.0`.
- `mise.toml`: local tool pin, currently `go = "1.25.1"`.

No `*_test.go` files were found during inspection.

## CLI Entry Points

The executable starts at `main.main`, which calls `rootCmd.Execute()`.

`rootCmd` in `main.go` defines persistent flags:

- `--debug`
- `--vmware-endpoint` required
- `--vmware-username` required
- `--vmware-password` required
- `--vmware-path` required
- `--compression-method`, enum-backed by `CompressionMethodOptsIds`
- `--availability-zone`
- `--volume-type`
- `--disk-bus-type`, enum-backed by `BusTypeOptsIds`
- `--vz-unsafe-volume-by-name`
- `--os-type`
- `--enable-qemu-guest-agent`

`migrateCmd` in `main.go` is registered as `migratekit migrate`. It calls:

1. `vmware_nbdkit.NewNbdkitServers(vddkConfig, vm)`
2. `servers.MigrationCycle(ctx, false)`

`cutoverCmd` in `main.go` is registered as `migratekit cutover`. It adds:

- `--flavor` required
- `--network-mapping` required, parsed by `cmd.NetworkMappingFlag.Set`
- `--security-groups`
- `--run-v2v`, default `true`
- a cutover-local `--availability-zone` required flag using the same backing
  variable as the root persistent `--availability-zone`

## Configuration and Environment Variables

VMware configuration is flag-based:

- Endpoint is passed as hostname or IP via `--vmware-endpoint`; `main.go` builds
  an HTTPS SDK URL with path `sdk`.
- Credentials are passed via `--vmware-username` and `--vmware-password`.
- VM inventory path is passed via `--vmware-path`.

OpenStack authentication is environment-based:

- `internal/openstack.NewClientSet` calls
  `openstack.AuthOptionsFromEnv()`. The exact accepted auth variables are
  delegated to Gophercloud; this repository documents examples that pass
  `OS_*` variables.
- The repository explicitly reads `OS_REGION_NAME` for block storage, compute,
  and networking endpoint selection.
- The repository explicitly reads `OS_INSECURE`; when it is `true`, TLS
  verification is disabled for OpenStack API clients.
- `README.md` examples pass `--env-file <(env | grep OS_)` to Docker.

Other runtime configuration:

- `internal/nbdkit.NbdkitBuilder.Build` sets `LD_LIBRARY_PATH` to
  `/usr/lib64/vmware-vix-disklib/lib64` before starting `nbdkit`.
- `internal/vmware_nbdkit.NbdkitServer.SyncToTarget` sets
  `LIBGUESTFS_BACKEND=direct` before running `virt-v2v-in-place`.
- The OpenStack target discovers the current instance UUID from
  `http://169.254.169.254/openstack/latest/meta_data.json` in
  `internal/openstack.GetCurrentInstanceUUID`.

## Main Migration Workflow

Both implemented subcommands, `migrate` and `cutover`, run
`rootCmd.PersistentPreRunE` first:

1. Enable debug logging if `--debug` is set.
2. Build a VMware SDK URL from endpoint, username, password, and `sdk` path.
3. Get the VMware endpoint certificate thumbprint with
   `vmware.GetEndpointThumbprint`.
4. Create a govmomi SOAP/VIM client with `soap.NewClient(endpointUrl, true)` and
   `vim25.NewClient`.
5. Wrap the VMware client round tripper with `keepalive.NewHandlerSOAP`.
6. Log in with `session.NewManager(vimClient).Login`.
7. Resolve the VM with `find.NewFinder(vimClient).VirtualMachine(ctx, path)`.
8. Read VM `config` properties and require `Config.ChangeTrackingEnabled`.
9. If a snapshot named `migratekit` already exists, prompt interactively to
   remove it or abort.
10. Store `vm`, `vddkConfig`, `volumeCreateOpts`, `vzUnsafeVolumeByName`,
    `osType`, and `enableQemuGuestAgent` in the command context.

The per-cycle data path starts in
`internal/vmware_nbdkit.NbdkitServers.MigrationCycle`:

1. `Start` creates a VMware snapshot named `migratekit`.
2. `Start` reads the snapshot hardware config and starts one `nbdkit` VDDK
   server for each `types.VirtualDisk`.
3. For each disk, `target.NewOpenStack` creates an OpenStack target.
4. `NbdkitServer.SyncToTarget` chooses full versus incremental copy using
   `target.NeedsFullCopy`.
5. The target volume is connected with `t.Connect`.
6. Data is copied to the local block path returned by `t.GetPath`.
7. The target change ID metadata is updated with `t.WriteChangeID`.
8. When the relevant defers have been registered, cleanup disconnects the
   target, stops `nbdkit`, and removes the VMware snapshot. Partial-start cases
   are called out under `Potential Bug-Hunting Targets`.

## VMware / vCenter Interaction

VMware access uses govmomi:

- `main.go` creates a `soap.Client`, `vim25.Client`, session manager, and
  finder.
- `vmware.GetEndpointThumbprint` opens a TLS connection to the endpoint and
  returns the SHA-1 certificate thumbprint in colon-separated uppercase hex.
- `main.go` validates VM CBT at the VM config level using
  `mo.VirtualMachine.Config.ChangeTrackingEnabled`.
- `internal/vmware.GetChangeID` extracts disk-level CBT `ChangeId` values from
  several VMware disk backing types:
  `VirtualDiskFlatVer2BackingInfo`, `VirtualDiskSparseVer2BackingInfo`,
  `VirtualDiskRawDiskMappingVer1BackingInfo`, and
  `VirtualDiskRawDiskVer2BackingInfo`.
- `NbdkitServers.createSnapshot` calls
  `VirtualMachine.CreateSnapshot(ctx, "migratekit", ..., false, false)`.
- Incremental copy calls
  `methods.QueryChangedDiskAreas` with the current target change ID, snapshot
  reference, device key, and rolling start offset.
- Cutover powers down the source using `vm.ShutdownGuest(ctx)` and then waits
  with `vm.WaitForPowerState(ctx, types.VirtualMachinePowerStatePoweredOff)`.

Unclear from code:

- Whether all supported VMware disk backing types are covered.
- Whether `WaitForPowerState` has an effective timeout in this usage.
- Whether the non-quiesced, memoryless snapshot mode is intentional for all
  supported guest workloads.

## VDDK, nbdkit, and libnbd Usage

VDDK is accessed through the external `nbdkit` VDDK plugin, not directly from
Go.

`internal/nbdkit.NbdkitBuilder.Build` constructs:

```bash
nbdkit --exit-with-parent --readonly --foreground \
  --unix=<temp>/nbdkit.sock \
  --pidfile=<temp>/nbdkit.pid \
  vddk \
  server=<endpoint-host> \
  user=<vmware-user> \
  password=<vmware-password> \
  thumbprint=<endpoint-thumbprint> \
  compression=<compression-method> \
  vm=moref=<vm-moref> \
  snapshot=<snapshot-moref> \
  transports=file:nbdssl:nbd \
  <disk-filename>
```

`internal/nbdkit.NbdkitServer.Start` starts the process and waits up to 10
seconds for the pidfile. `LibNBDExportName` returns an `nbd+unix://` URI using
the temp Unix socket.

Full copies use the external `nbdcopy` command through
`internal/nbdcopy.Run`. The source is the `nbdkit` libnbd URI; the destination
is the local target block path. `--destination-is-zero` is added when the target
volume was newly created.

Incremental copies use Go bindings from `libguestfs.org/libnbd`:

- `libnbd.Create`
- `handle.ConnectUri(s.Nbdkit.LibNBDExportName())`
- `handle.Pread` for changed regions returned by VMware CBT

Writes go to the target path opened with
`os.OpenFile(path, os.O_WRONLY|os.O_EXCL|syscall.O_DIRECT, 0644)`.

## OpenStack Volume / Server Interaction

OpenStack API access uses Gophercloud.

`internal/openstack.NewClientSet` creates:

- Block Storage v3 client
- Compute v2 client
- Network v2 client

Volumes are found by `ClientSet.GetVolumeForDisk`:

- Current format: name from `VolumeName(vm, disk)` plus metadata
  `migrate_kit=true`, `vm=<vm-moref>`, and `disk=<disk-key>`.
- Optional unsafe format: with `--vz-unsafe-volume-by-name`, lookup uses only
  the volume name.
- Deprecated fallback: `GetVolumeListForDiskOld` uses `VolumeNameOld` and
  `disk.DiskObjectId`.

`internal/target.OpenStack.Connect`:

1. Finds the Cinder volume, or creates it if `GetVolumeForDisk` returns
   `openstack.ErrorVolumeNotFound`.
2. For newly created volumes, supplies volume metadata
   `migrate_kit=true`, `vm=<vm-moref>`, and `disk=<disk-key>`, plus optional
   SCSI disk bus metadata.
3. For newly created volumes, marks the volume bootable with
   `volumes.SetBootable`.
4. For newly created volumes, reads VMware firmware, guest ID, and boot options
   to set selected image metadata.
5. For newly created volumes, sets `os_type` from `--os-type`, or performs
   simple auto detection where VMware guest IDs containing `windows` map to
   `windows` and everything else defaults to `linux`.
6. For newly created volumes, optionally sets `hw_qemu_guest_agent=yes`.
7. For newly created volumes, sets UEFI and secure-boot metadata when detected
   from VMware config.
8. If no local device path is already found, attaches the volume to the current
   OpenStack instance using Nova volume attachments and the current instance
   UUID from metadata service.
9. When attaching, waits up to two minutes for a matching local device under
   `/dev/disk/by-id`.

`findDevice` matches local devices by checking whether the by-id name contains
the first 18 characters of the volume ID, then resolves the symlink.

`internal/target.OpenStack.Disconnect` detaches attached volumes and waits up to
60 seconds for the volume to return to `available`.

`internal/openstack.ClientSet.EnsurePortsForVirtualMachine` creates or reuses
Neutron ports for each VMware virtual NIC according to `--network-mapping`.

`internal/openstack.ClientSet.CreateResourcesForVirtualMachine` creates the
Nova server from the migrated volumes:

- VM name comes from VMware `config.name`.
- Flavor comes from `--flavor`.
- Networks are the Neutron port IDs prepared earlier.
- Block devices are all VMware disks in device iteration order, each using the
  matching migrated Cinder volume.
- Availability zone comes from `--availability-zone`.
- It waits up to five minutes for server status `ACTIVE`.

## virt-v2v Usage

`virt-v2v-in-place` is invoked from
`internal/vmware_nbdkit.NbdkitServer.SyncToTarget` only when `runV2V` is true.

Cutover passes the `--run-v2v` value to the final migration cycle. Inside
`NbdkitServers.MigrationCycle`, `runV2V` is forced to `false` for every disk
except index `0`, so `virt-v2v-in-place` only runs against the first migrated
disk.

The command is:

```bash
virt-v2v-in-place -i disk <target-device-path>
```

When `--debug` is enabled, it adds `-v -x`.

After a successful `virt-v2v-in-place`, the code writes an empty VMware change
ID to the target by calling `t.WriteChangeID(ctx, &vmware.ChangeID{})`. That
appears to force a later migration cycle to perform a full copy, but the
intended operational meaning is not documented in code.

## Migration vs Cutover Flow

`migratekit migrate`:

1. Runs the shared VMware/OpenStack setup in `PersistentPreRunE`.
2. Creates `NbdkitServers`.
3. Runs one `MigrationCycle` with `runV2V=false`.
4. Leaves the source VMware VM powered on.

`migratekit cutover`:

1. Runs the shared setup.
2. Creates OpenStack clients.
3. Verifies the requested flavor with `flavors.Get`.
4. Builds `PortCreateOpts` from `--security-groups`.
5. Ensures Neutron ports exist for all VMware NICs.
6. Runs an online `MigrationCycle` with `runV2V=false`.
7. Checks source VM power state.
8. If powered on, calls `ShutdownGuest` and waits for powered off.
9. Runs a final `MigrationCycle` with `runV2V=<--run-v2v>`.
10. Creates the OpenStack server from migrated volumes and prepared ports.

Cutover does not appear to power the VMware source VM back on after failure.
That may be intentional, but it is not documented in code.

## Error Handling and Retry Behavior

Most functions return errors directly to Cobra, and `main.main` exits with code
1 if command execution returns an error.

Observed behavior:

- VMware VM not found is special-cased in `PersistentPreRunE`: it logs all VM
  inventory paths and calls `os.Exit(1)`.
- Existing `migratekit` snapshot is handled interactively. The user may delete
  it, or the command aborts.
- `MigrationCycle` defers `s.Stop(ctx)` after `Start` succeeds. If cleanup
  fails, it logs fatal and exits.
- `SyncToTarget` defers `t.Disconnect(ctx)` after `Connect` succeeds.
- Signal handlers attempt cleanup on `os.Interrupt` and `SIGTERM` in both
  `NbdkitServers.Start` and `NbdkitServer.SyncToTarget`.
- Volume creation waits 60 seconds for `available`.
- Volume attach waits two minutes for a local device.
- Volume detach waits 60 seconds for `available`.
- Server create waits five minutes for `ACTIVE`.
- `nbdkit` startup waits 10 seconds for its pidfile.
- No explicit retry loops were found in this repository for VMware API calls,
  OpenStack API calls, `nbdcopy`, libnbd reads, block-device writes,
  `virt-v2v-in-place`, or Nova server creation.
- Incremental fallback is based on change ID state. `target.NeedsFullCopy`
  chooses full copy when the target does not exist, when
  `GetCurrentChangeID` returns `vmware.ErrInvalidChangeID`, when the current
  change ID is `nil`, or when the stored change ID UUID differs from the
  current snapshot change ID UUID. For an existing volume with no `change_id`
  metadata, `OpenStack.GetCurrentChangeID` returns an empty `ChangeID`; that
  should still lead to a full copy in normal CBT cases because the empty UUID
  does not match the snapshot change ID UUID.

## External Runtime Dependencies

Development workflow in `AGENTS.md` and `hack/dev-shell.sh` expects:

```bash
docker run -it --rm --privileged \
  --network host \
  -v /dev:/dev \
  -v /usr/lib64/vmware-vix-disklib/:/usr/lib64/vmware-vix-disklib:ro \
  -v "$(pwd):/app" \
  --entrypoint /bin/bash \
  fedora:40
```

Inside that container, the documented packages are:

```bash
dnf install -y \
  nbdkit \
  nbdkit-vddk-plugin \
  libnbd \
  libnbd-devel \
  golang \
  virt-v2v
```

Runtime dependencies visible in code and docs:

- Docker or an equivalent container runtime for the documented workflow.
- Privileged container mode and host networking.
- `/dev` mounted into the container, because target volumes are discovered and
  written through host block devices.
- VMware VDDK under `/usr/lib64/vmware-vix-disklib/`.
- `nbdkit` and `nbdkit-vddk-plugin`.
- `nbdcopy`, because `internal/nbdcopy.Run` shells out to a command named
  `nbdcopy`. The repository does not state which documented Fedora package
  provides it.
- `libnbd` runtime and Go bindings.
- `virt-v2v-in-place`.
- OpenStack API access through the environment variables accepted by
  Gophercloud, with repository examples using `OS_*` variables.
- OpenStack metadata service access from the host/container running migratekit.
- Permissions to attach/detach Cinder volumes to the current OpenStack
  instance.
- VMware/vCenter permissions for CBT, snapshots, disk lease/read access, and
  guest shutdown, as documented in `README.md`.

Repository version notes:

- `Dockerfile` uses `fedora:44`.
- `hack/dev-shell.sh` and `AGENTS.md` use `fedora:40`.
- `go.mod` declares `go 1.25.0`.
- `mise.toml` pins `go = "1.25.1"`.

Whether the documented Fedora 40 development package set provides a Go version
compatible with `go.mod` is not determined from repository files alone.

## Questions / Follow-ups

- Should the development container remain `fedora:40`, or should it align with
  the `fedora:44` production `Dockerfile`?
- Which Go version is authoritative: `go.mod` `1.25.0`, `mise.toml` `1.25.1`,
  or the distro `golang` package in the required dev container?
- Is `virt-v2v-in-place` intentionally run only on the first disk?
- After successful `virt-v2v-in-place`, is writing an empty `change_id`
  intentional to force a future full copy?
- Is cutover expected to leave the source VMware VM powered off if any later
  step fails?
- Are non-quiesced snapshots acceptable for all supported workloads?
- Should `ShutdownGuest` have a timeout or fallback power-off behavior?
- Is `--vz-unsafe-volume-by-name` still required for a supported target cloud,
  or should it be isolated as a local workaround?
- Are multiple disks expected to preserve VMware boot order when iterating
  `devices.SelectByType((*types.VirtualDisk)(nil))`?
- Should OpenStack ports created before cutover be cleaned up if migration or
  shutdown fails?
- Does the command need a noninteractive mode for automation when a stale
  `migratekit` snapshot exists?
- Does the documented Fedora package set install the `nbdcopy` executable
  directly or indirectly, or should the dev/runtime dependency list name its
  provider explicitly?

## Potential Bug-Hunting Targets

- Credential exposure: `internal/nbdkit.NbdkitBuilder.Build` passes the VMware
  password as a command-line argument to `nbdkit`; debug logging also logs the
  command object. Check process listings and logs for credential leakage.
- Direct I/O alignment: incremental writes open the target with `O_DIRECT`, but
  buffers are created with `make([]byte, chunkSize)`. Confirm libnbd read
  buffers and `WriteAt` calls satisfy platform alignment requirements.
- Device matching: `findDevice` matches only `volumeID[:18]` inside
  `/dev/disk/by-id` names. Test collision and stale symlink scenarios.
- Cleanup masking original errors: deferred cleanup uses `log.Fatal` in
  `MigrationCycle`, which can exit the process and hide the original copy error.
- Signal handling accumulation: both `NbdkitServers.Start` and
  `NbdkitServer.SyncToTarget` call `signal.Notify` and start goroutines for each
  cycle/disk. Check repeated cycles and multi-disk behavior.
- Snapshot cleanup: if `NbdkitServers.Start` creates the VMware snapshot and
  then fails before returning successfully, `MigrationCycle` has not registered
  its deferred `Stop` call yet. Confirm whether partial-start failures can leave
  stale `migratekit` snapshots.
- Existing snapshot detection: `main.go` ignores the error return from
  `vm.FindSnapshot(ctx, "migratekit")`. Confirm how govmomi reports lookup
  errors and whether ignoring them can hide a stale snapshot or connectivity
  issue.
- `nbdkit` temp cleanup: `Stop` removes the socket but not the pidfile or temp
  directory created by `os.MkdirTemp`.
- Incremental correctness: test missing, invalid, empty, stale, and mismatched
  `change_id` metadata on target volumes.
- Full-copy safety: confirm `--destination-is-zero` is only used for truly clean
  newly created volumes and never for reused volumes.
- Metadata mutation safety: `OpenStack.WriteChangeID` assigns into
  `volume.Metadata` before calling `volumes.Update`; confirm the map is never
  `nil` for volumes returned by Gophercloud.
- OpenStack metadata service dependency: `GetCurrentInstanceUUID` has no HTTP
  timeout and assumes access to `169.254.169.254`. Test behavior outside an
  OpenStack instance or with metadata service failure.
- Volume attach/detach timing: hard-coded two-minute attach and 60-second detach
  waits may be short for some clouds.
- Cutover rollback: no rollback path is apparent after source shutdown,
  post-shutdown migration failure, `virt-v2v-in-place` failure, or Nova boot
  failure.
- Network mapping: `EnsurePortsForVirtualMachine` errors on the first VMware NIC
  without a mapping. Test multi-NIC VMs and duplicate/extra mapping inputs.
- Flag behavior: `--availability-zone` is registered as both a root persistent
  flag and a cutover-local required flag with the same backing variable. Confirm
  Cobra help, parsing, and required-flag behavior.
- OS metadata detection: `--os-type=auto` only checks whether VMware guest ID
  contains `windows`; all other values become `linux`.
- VMware wait behavior: confirm whether `vm.WaitForPowerState` can block
  indefinitely after `ShutdownGuest` if VMware Tools is absent or guest shutdown
  fails silently.
- No tests: there are no `*_test.go` files, so parser behavior, change ID
  fallback, target selection, and resource creation order are currently
  unverified by automated tests in this tree.
