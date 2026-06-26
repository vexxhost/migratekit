# Multi-Disk Detach Timeout Investigation

## Summary

The multi-disk migration path is serial at the disk-copy layer: each VMware disk
is copied by `internal/vmware_nbdkit.NbdkitServers.MigrationCycle`, and the next
disk is not processed until `NbdkitServer.SyncToTarget` returns for the current
disk.

The most likely bug area is OpenStack volume detach handling plus timeout/error
propagation, not VMware copy sequencing. The code attaches each target Cinder
volume to the current OpenStack instance, copies data through the local block
device, and then calls `OpenStack.Disconnect` through a deferred call. That
detach path:

- re-discovers the volume and local device instead of tracking the attachment
  returned by `volumeattach.Create`,
- calls Nova detach using `volume.ID`,
- waits only for the Cinder volume status to become `available`,
- does not confirm the Nova attachment is gone,
- does not confirm the local `/dev/disk/by-id` entry disappeared,
- uses a fixed 60 second detach wait, and
- has its normal deferred error ignored by `SyncToTarget`.

Those behaviors match a failure mode where the first disk copy finishes, detach
is slow or incomplete, the process blocks for the short wait, may silently move
on after a detach error, and later disk attachment or final server creation needs
manual cleanup.

## Reproduction Scenario

Code-backed scenario to exercise:

1. Run `migratekit migrate` or `migratekit cutover` for a VMware VM with at
   least two virtual disks.
2. Run from an OpenStack instance that can attach Cinder volumes to itself.
3. Let the first disk copy complete.
4. Observe the detach of the first migrated Cinder volume from the migration
   worker instance.
5. Continue into the second disk copy.

The reported field behavior is that after one disk copy completes, the process
has trouble detaching the copied disk/volume, confirming detach completion, and
moving on to the next drive. Manual intervention is sometimes required, or the
process times out.

## Expected Behavior

For each disk in a multi-disk migration cycle:

1. Resolve or create the matching Cinder volume for the VMware disk.
2. Attach that volume to the current migration worker instance.
3. Wait until the local block device for that exact volume is usable.
4. Copy full or incremental data to that local block device.
5. Write the VMware CBT change ID metadata.
6. Detach that exact attachment from the migration worker.
7. Wait until the detach has completed in Nova, Cinder, and local device state.
8. Move to the next disk only after the prior disk is no longer attached to the
   migration worker.

For cutover, all migrated volumes should be detached from the migration worker
before `CreateResourcesForVirtualMachine` attempts to boot the destination Nova
server from those volumes.

## Observed Behavior

The user-reported behavior is:

- After one disk copy completes, the process sometimes struggles to detach the
  copied OpenStack volume.
- Detach confirmation can time out.
- Moving on to the next drive can require manual intervention.

This investigation did not reproduce the behavior live. Findings are based on
static code inspection.

## Relevant Files / Functions

- `main.go`
  - `migrateCmd.RunE` at lines 219-233 starts one `MigrationCycle`.
  - `cutoverCmd.RunE` at lines 245-325 starts an online `MigrationCycle`, shuts
    down the source VM, starts a final `MigrationCycle`, then creates the Nova
    server.
- `internal/vmware_nbdkit/vmware_nbdkit.go`
  - `NbdkitServers.Start` at lines 76-135 creates one VMware snapshot and starts
    one `nbdkit` server per VMware disk.
  - `NbdkitServers.MigrationCycle` at lines 175-203 loops over
    `s.Servers` and calls `server.SyncToTarget` for each disk.
  - `NbdkitServer.FullCopyToTarget` at lines 206-227 runs `nbdcopy`.
  - `NbdkitServer.IncrementalCopyToTarget` at lines 229-308 reads changed
    regions through libnbd and writes to the target block device.
  - `NbdkitServer.SyncToTarget` at lines 311-390 connects the target, copies,
    optionally runs `virt-v2v-in-place`, writes change ID metadata, and defers
    target disconnect.
- `internal/target/openstack.go`
  - `OpenStack.Connect` at lines 72-235 creates/attaches the target Cinder
    volume and waits for a local device.
  - `OpenStack.GetPath` at lines 262-274 maps a volume ID to a local device path.
  - `OpenStack.Disconnect` at lines 276-310 detaches the target volume and waits
    for Cinder `available`.
  - `findDevice` at lines 52-70 searches `/dev/disk/by-id` for the first 18
    characters of the volume ID.
- `internal/openstack/client.go`
  - `ClientSet.GetVolumeForDisk` at lines 96-140 finds the migrated Cinder
    volume for a VMware disk.
  - `ClientSet.CreateResourcesForVirtualMachine` at lines 245-296 creates the
    final Nova server from all migrated volumes after final cutover sync.
- `internal/openstack/util.go`
  - `GetCurrentInstanceUUID` at lines 18-42 reads the migration worker UUID from
    the OpenStack metadata service.

## Execution Flow

### `migrate`

1. `main.go:migrateCmd.RunE` gets the VMware VM and VDDK config from context.
2. It creates `NbdkitServers`.
3. It calls `servers.MigrationCycle(ctx, false)`.
4. `MigrationCycle` calls `Start`, which creates one snapshot and one `nbdkit`
   VDDK server per VMware disk.
5. `MigrationCycle` loops over `s.Servers` sequentially.
6. For each disk, it creates an OpenStack target with `target.NewOpenStack`.
7. It calls `server.SyncToTarget`.
8. `SyncToTarget` decides full vs incremental copy, calls `t.Connect`, defers
   `t.Disconnect`, copies the disk, writes `change_id`, and returns.
9. Only after `SyncToTarget` returns does `MigrationCycle` move to the next disk.
10. After all disks, `MigrationCycle` stops all `nbdkit` servers and removes the
    VMware snapshot.

### `cutover`

1. `cutoverCmd.RunE` ensures OpenStack flavor and Neutron ports exist.
2. It runs one online `MigrationCycle(ctx, false)`.
3. It shuts down the VMware source VM with `ShutdownGuest` and waits for powered
   off.
4. It runs a final `MigrationCycle(ctx, enablev2v)`.
5. In the final cycle, `MigrationCycle` forces `runV2V=false` for every disk
   except disk index `0`.
6. After the final cycle returns, `CreateResourcesForVirtualMachine` creates the
   destination Nova server from all migrated Cinder volumes.

Detach behavior is shared by `migrate`, the online cutover cycle, and the final
cutover cycle.

## Where Each Disk Is Copied

Each disk is copied inside `NbdkitServer.SyncToTarget`:

- Full copy path:
  - `SyncToTarget` calls `FullCopyToTarget`.
  - `FullCopyToTarget` calls `nbdcopy.Run`.
  - `nbdcopy.Run` shells out to `nbdcopy --progress=3 <nbdkit-uri> <path>`.
- Incremental copy path:
  - `SyncToTarget` calls `IncrementalCopyToTarget`.
  - `IncrementalCopyToTarget` calls VMware `QueryChangedDiskAreas`.
  - It reads changed ranges through `libnbd`.
  - It writes ranges to the local target path with `fd.WriteAt`.

The target path comes from `t.GetPath(ctx)`, implemented by
`OpenStack.GetPath`, which maps the Cinder volume ID to a local `/dev/disk/by-id`
symlink.

## Where Detach / Cleanup Happens

### OpenStack Volume Attach

`OpenStack.Connect`:

1. Finds or creates the target Cinder volume.
2. Calls `GetPath`.
3. If no path exists, gets the current migration worker UUID through
   `GetCurrentInstanceUUID`.
4. Calls `volumeattach.Create(ctx, computeClient, instanceUUID, CreateOpts{
   VolumeID: volume.ID })`.
5. Polls `findDevice(volume.ID)` once per second until a local device appears or
   two minutes pass.

The returned Nova volume attachment from `volumeattach.Create(...).Extract()` is
discarded.

### OpenStack Volume Detach

`OpenStack.Disconnect`:

1. Re-finds the Cinder volume with `GetVolumeForDisk`.
2. Calls `findDevice(volume.ID)`.
3. If `findDevice` returns an empty string, returns nil without issuing a Nova
   detach request.
4. If a device path exists, gets the current migration worker UUID.
5. Calls `volumeattach.Delete(ctx, computeClient, instanceUUID, volume.ID)`.
6. Waits up to 60 seconds for the Cinder volume status to become `available`.

There is no Nova attachment-list check, no explicit attachment ID tracking, and
no wait for local device removal.

### Other Cleanup

- The migrated Cinder volumes are not temporary in this flow. They remain after
  `Disconnect` and are later reused by cutover server creation. The temporary
  resource is the attachment between each migrated volume and the migration
  worker instance.
- `virt-v2v-in-place` runs only when `runV2V` is true, and `cmd.Run()` must
  return before `SyncToTarget` reaches its deferred disconnect.
- `nbdkit` VDDK processes are not stopped per disk. They are all stopped after
  every disk in the migration cycle finishes, in `NbdkitServers.Stop`.
- The code does not create a temporary OpenStack server for conversion. The
  final destination Nova server is created only after final cutover
  `MigrationCycle` returns.

## How Detach Completion Is Confirmed

Detach completion is confirmed only by:

```go
ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
defer cancel()

err = volumes.WaitForStatus(ctx, t.ClientSet.BlockStorage, volume.ID, "available")
```

This is Cinder status confirmation only. The code does not confirm:

- the Nova volume attachment was removed,
- the attachment ID used for deletion was the actual attachment ID returned by
  Nova,
- the volume has no remaining attachments,
- `/dev/disk/by-id` no longer contains the local device entry, or
- the kernel has finished releasing the block device.

## Timeout / Retry Behavior

- New volume creation waits 60 seconds for Cinder `available`.
- Volume attach waits two minutes for `findDevice(volume.ID)` to return a local
  path.
- Volume detach waits 60 seconds for Cinder `available`.
- There is no retry around `volumeattach.Delete`.
- There is no retry around a detach wait timeout.
- There is no backoff or longer cloud-configurable timeout.
- `GetCurrentInstanceUUID` uses `http.Client{}` with no explicit timeout.
- The normal deferred `t.Disconnect(ctx)` return value in `SyncToTarget` is
  ignored.

Because `defer t.Disconnect(ctx)` ignores the returned error, a detach timeout
can delay the current disk by 60 seconds and then be silently discarded if the
copy path itself succeeded.

## Suspected Root Cause

Most likely category: **OpenStack volume detach handling** plus
**timeout/retry logic**.

Contributing category: **per-disk state tracking**.

Less likely categories:

- **Migration loop sequencing**: the loop is sequential and waits for
  `SyncToTarget` to return before starting the next disk.
- **virt-v2v helper cleanup**: `migrate` does not run `virt-v2v-in-place`, and
  cutover runs it only on disk index `0` after the copy command returns.
  `virt-v2v` may increase detach latency for the first final-cutover disk, but
  it is not required to explain the same issue during `migrate`.

The strongest code-backed hypothesis is:

1. The code does not track the attachment created for a disk.
2. Detach uses `volume.ID` and current instance UUID, not a stored attachment
   object.
3. Detach confirmation waits only for Cinder `available`, for only 60 seconds.
4. Detach errors are ignored by the defer in the normal copy path.
5. Multi-disk migration depends on each disk being cleanly detached before the
   next disk can safely attach and copy.

That combination can leave the migration worker with a still-attached or
still-detaching first disk while the process moves to the next disk or reaches
final server creation.

## Evidence From The Code

- Multi-disk processing is serial:
  `internal/vmware_nbdkit/vmware_nbdkit.go:187-200` loops over `s.Servers` and
  calls `server.SyncToTarget`.
- The target disconnect is deferred and its error is ignored:
  `internal/vmware_nbdkit/vmware_nbdkit.go:322-326`.
- The signal-handler disconnect path logs fatal on disconnect errors, but the
  normal deferred path does not inspect or log the error:
  `internal/vmware_nbdkit/vmware_nbdkit.go:328-340`.
- Attach discards the returned attachment:
  `internal/target/openstack.go:198-200` calls `volumeattach.Create(...).Extract()`
  and assigns only to `_`.
- Detach re-discovers the volume and local path:
  `internal/target/openstack.go:276-284`.
- Detach is skipped entirely if `findDevice` does not find a local path:
  `internal/target/openstack.go:284-289`.
- Detach calls Nova with `volume.ID`:
  `internal/target/openstack.go:295`.
- Detach completion waits only for Cinder `available`:
  `internal/target/openstack.go:300-305`.
- Local device discovery uses a partial volume-ID substring:
  `internal/target/openstack.go:52-70`.
- Final cutover server creation assumes all prior disk detach work completed:
  `main.go:309-317` runs the final migration cycle and then calls
  `CreateResourcesForVirtualMachine`.
- `CreateResourcesForVirtualMachine` attaches all migrated volumes as boot
  block devices for the destination server:
  `internal/openstack/client.go:257-282`.

## Risks

- A detach timeout can be hidden because the normal defer ignores
  `Disconnect` errors.
- Hidden detach failures can leave volumes attached to the migration worker.
- A later disk attach may fail or stall if the migration worker or cloud is
  still processing the prior detach.
- Final Nova server creation can fail if migrated volumes remain attached to the
  migration worker after final cutover sync.
- Re-discovering by local device path can skip detach when the Nova/Cinder
  attachment still exists but the local `/dev/disk/by-id` entry is missing.
- Waiting only for Cinder `available` may not be enough to prove local device
  cleanup is complete.
- A hard-coded 60 second detach timeout may be too short for some OpenStack
  backends, multipath configurations, or large/slow volumes.
- If `volumeattach.Delete` requires an attachment ID that differs from
  `volume.ID` in a target cloud, the code has no fallback because it discards
  the attachment returned by `Create`.

## Smallest Safe Fix Proposal

Do not refactor unrelated migration logic. Keep the per-disk serial model, but
make OpenStack detach explicit, observable, and blocking.

Suggested minimal change set:

1. Preserve disconnect errors from `SyncToTarget`.
   - Use a named return error or explicit cleanup block so
     `t.Disconnect(ctx)` errors are returned when the copy itself succeeded.
   - Log the volume ID, disk key, local path, and detach failure.
2. Track or resolve the real Nova attachment.
   - Store the attachment returned by `volumeattach.Create`, or list current
     server volume attachments and find the one matching `volume.ID`.
   - Use the correct attachment identifier for `volumeattach.Delete`.
3. Confirm detach through more than Cinder status.
   - Wait until the Nova attachment is gone.
   - Wait until Cinder reports `available`.
   - Wait until `findDevice(volume.ID)` no longer returns a local device.
4. Extend or configure detach timeout.
   - A 60 second fixed timeout is likely too tight for real clouds.
   - Use a longer default such as five minutes, or add a flag/config value.
5. Keep the next disk blocked until detach confirmation succeeds.
   - If detach cannot be confirmed, return an error instead of silently moving
     to the next disk.

The very first code change should probably be preserving and returning
`Disconnect` errors. That makes the failure visible without changing the copy
algorithm.

## Tests That Should Be Added

Unit-level tests, likely requiring small interfaces or fakes around the target:

- Multi-disk sequencing test:
  - Disk 1 `Connect`, copy, `Disconnect` must complete before disk 2 `Connect`.
- Disconnect error propagation test:
  - If copy succeeds but `Disconnect` fails, `SyncToTarget` should return the
    disconnect error.
- Disconnect wait test:
  - `Disconnect` should not return success until attachment gone, Cinder status
    available, and local device path gone.
- Detach timeout test:
  - Simulate a volume that never reaches available or whose attachment remains;
    assert the error is returned and logged.
- Already-detached/local-missing test:
  - If local device path is missing but Nova/Cinder still show attachment,
    `Disconnect` should still detach or report the inconsistency.
- Multi-disk cutover test:
  - Final server creation should not be attempted until every migrated volume
    has been detached from the migration worker.
- `virt-v2v` final-cycle test:
  - When `runV2V=true`, disk index `0` runs conversion and still performs the
    same detach confirmation before disk index `1`.

Integration/manual tests:

- OpenStack integration test with a source VM containing at least two disks and
  migrated target Cinder volumes.
- Slow-detach simulation or backend where detach commonly exceeds 60 seconds.

## Manual Validation Steps

1. Start the required Fedora Docker development environment from `AGENTS.md`.
2. Install the documented dependencies, including `nbdkit`, `nbdkit-vddk-plugin`,
   `libnbd`, `libnbd-devel`, `golang`, and `virt-v2v`.
3. Confirm VDDK exists at `/usr/lib64/vmware-vix-disklib/`.
4. Prepare a VMware VM with at least two virtual disks and CBT enabled.
5. Prepare OpenStack credentials and run `migratekit migrate --debug ...`.
6. In another shell, watch the migration worker attachments:

   ```bash
   openstack server volume list <migration-worker-server-id>
   openstack volume show <volume-id>
   ls -l /dev/disk/by-id/
   ```

7. After disk 1 copy completes, confirm:
   - Nova attachment for disk 1 is gone.
   - Cinder volume for disk 1 is `available`.
   - `/dev/disk/by-id` no longer exposes disk 1.
   - Disk 2 attach starts only after disk 1 detach is complete.
8. Repeat with `migratekit cutover --debug ... --run-v2v=true`.
9. Before final Nova server creation, confirm every migrated volume is detached
   from the migration worker and available.
10. Repeat on a cloud/backend known to have slow detach behavior and confirm the
    process returns a useful error instead of needing manual cleanup.
