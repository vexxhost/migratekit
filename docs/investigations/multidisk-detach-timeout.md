# Multi-Disk Detach Timeout Investigation

## Summary

The multi-disk migration path is serial at the disk-copy layer: each VMware disk
is copied by `internal/vmware_nbdkit.NbdkitServers.MigrationCycle`, and the next
disk is not processed until `NbdkitServer.SyncToTarget` returns for the current
disk.

The log-backed root cause is most likely an OpenStack attachment-state race, not
VMware disk sequencing. The successful and failing paths both attach a target
Cinder volume, wait for a local `/dev/disk/by-id` path, copy data, and call
`OpenStack.Disconnect`. The first divergence happens when
`volumeattach.Delete` is called:

- successful path: Nova accepts the detach request, then
  `waitForVolumeDetached` polls until Cinder status, attachment count, and local
  device state indicate detach completion;
- failing path: Nova rejects the detach request before polling starts because
  the volume is not yet in the `in-use` / attached state required for detach.

The strongest explanation is that `OpenStack.Connect` treats local device
appearance as sufficient attach readiness. That proves the worker OS sees the
block device, but it does not prove the Nova/Cinder control plane has finished
transitioning the volume attachment to attached. If the subsequent copy is very
fast, `Disconnect` can attempt detach while OpenStack still reports the volume as
not detachable.

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

The provided logs show two forms of the same behavior:

- In the 2-disk run, the first disk reaches detach immediately after a fast
  incremental copy. Nova rejects the DELETE before detach polling starts.
- In the 7-disk run, the first two disks detach successfully because their copy
  windows are long enough for attachment state to settle. The third disk copies
  almost instantly and fails at the same DELETE call.

The 7-disk log is therefore a source of successful detach examples, not a fully
successful end-to-end migration log.

This investigation did not reproduce the behavior live. Findings are based on
static code inspection plus the provided debug logs. The debug logs contain
sensitive command-line data, so this document references line numbers and state
transitions without quoting full command lines.

## Relevant Files / Functions

- `main.go`
  - `migrateCmd.RunE` at lines 219-233 starts one `MigrationCycle`.
  - `cutoverCmd.RunE` at lines 245-325 starts an online `MigrationCycle`, shuts
    down the source VM, starts a final `MigrationCycle`, then creates the Nova
    server.
- `internal/vmware_nbdkit/vmware_nbdkit.go`
  - `NbdkitServers.Start` creates one VMware snapshot and starts
    one `nbdkit` server per VMware disk.
  - `NbdkitServers.MigrationCycle` loops over
    `s.Servers` and calls `server.SyncToTarget` for each disk.
  - `NbdkitServer.FullCopyToTarget` runs `nbdcopy`.
  - `NbdkitServer.IncrementalCopyToTarget` reads changed
    regions through libnbd and writes to the target block device.
  - `NbdkitServer.SyncToTarget` connects the target, copies,
    optionally runs `virt-v2v-in-place`, writes change ID metadata, and defers
    target disconnect.
- `internal/target/openstack.go`
  - `OpenStack.Connect` creates/attaches the target Cinder volume and waits for
    a local device.
  - `OpenStack.GetPath` maps a volume ID to a local device path.
  - `OpenStack.Disconnect` detaches the target volume and calls
    `waitForVolumeDetached`.
  - `waitForVolumeDetached` polls Cinder status, Cinder attachment count, and
    local device visibility.
  - `findDevice` searches `/dev/disk/by-id` for the first 18
    characters of the volume ID.
- `internal/openstack/client.go`
  - `ClientSet.GetVolumeForDisk` finds the migrated Cinder volume for a VMware
    disk.
  - `ClientSet.CreateResourcesForVirtualMachine` creates the final Nova server
    from all migrated volumes after final cutover sync.
- `internal/openstack/util.go`
  - `GetCurrentInstanceUUID` reads the migration worker UUID from the OpenStack
    metadata service.

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
discarded. The attach wait does not poll Cinder volume status, Cinder attachment
count, or Nova's server volume-attachment list before returning to the copy
path.

### OpenStack Volume Detach

`OpenStack.Disconnect`:

1. Re-finds the Cinder volume with `GetVolumeForDisk`.
2. Calls `findDevice(volume.ID)`.
3. If Cinder already reports `available`, has zero attachments, and the local
   device is absent, returns success.
4. If Cinder already reports `available` with zero attachments but the local
   device is still present, waits for the local device to disappear.
5. Otherwise, gets the current migration worker UUID.
6. Calls `volumeattach.Delete(ctx, computeClient, instanceUUID, volume.ID)`.
7. Waits up to five minutes for Cinder status `available`, zero Cinder
   attachments, and no local `/dev/disk/by-id` match.

There is no Nova attachment-list check before issuing DELETE and no explicit
attachment ID tracking. If Nova rejects the DELETE, the post-DELETE detach wait
never starts.

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

After Nova accepts a detach DELETE, detach completion is confirmed by
`waitForVolumeDetached`:

```go
volumeDetachComplete(status, attachmentCount, devicePath)
```

That requires:

- Cinder status `available`,
- Cinder attachment count `0`, and
- no local `/dev/disk/by-id` path for the target volume.

The code still does not confirm attach readiness symmetrically before copy. In
the failing logs, the error occurs before `waitForVolumeDetached` starts, so
post-DELETE polling cannot protect against a premature DELETE request.

## Timeout / Retry Behavior

- New volume creation waits 60 seconds for Cinder `available`.
- Volume attach waits two minutes for `findDevice(volume.ID)` to return a local
  path.
- Volume detach waits five minutes after Nova accepts DELETE.
- There is no retry around `volumeattach.Delete`.
- There is no retry around a detach wait timeout.
- There is no backoff or longer cloud-configurable timeout.
- `GetCurrentInstanceUUID` uses `http.Client{}` with no explicit timeout.
- The normal deferred `t.Disconnect(ctx)` return value in `SyncToTarget` is
  propagated by the current code, so detach failures stop the migration cycle.

Because there is no retry around `volumeattach.Delete`, a transient
not-yet-attached OpenStack state becomes a hard migration failure even if the
volume would become detachable seconds later.

## Root Cause Analysis

### Line-by-Line Path Comparison

The logs share the same execution path until the first Nova detach DELETE after
a disk copy.

Successful detach path, from the 7-disk log:

1. Disk `2000` attaches volume `9c2ca...`.
2. `OpenStack.Connect` reports a local device at `/dev/vdf`.
3. Incremental copy starts and completes.
4. `SyncToTarget` begins deferred disconnect for disk `2000`.
5. `OpenStack.Disconnect` logs `Detaching volume`.
6. Nova accepts `volumeattach.Delete`.
7. `waitForVolumeDetached` logs `Waiting for volume detach to complete`.
8. Polling observes `status=detaching`, `attachments=1`, and `device=/dev/vdf`.
9. Polling completes and logs `Volume detach completed`.
10. `SyncToTarget` logs `Target disk disconnected`.
11. `MigrationCycle` moves to the next disk.

Failing detach path, from the 2-disk log:

1. Disk `2000` attaches volume `1acd...`.
2. `OpenStack.Connect` reports a local device at `/dev/vdf`.
3. Incremental copy starts and completes very quickly.
4. `SyncToTarget` begins deferred disconnect for disk `2000`.
5. `OpenStack.Disconnect` logs `Detaching volume`.
6. Nova rejects `volumeattach.Delete` with HTTP 400 before
   `waitForVolumeDetached` starts.
7. `SyncToTarget` logs `Failed to disconnect target disk`.
8. `MigrationCycle` returns the error and the snapshot cleanup runs.

The 7-disk log later repeats the same failing path on disk `2017`: attach local
device, very fast copy, `Detaching volume`, immediate HTTP 400 from Nova, and no
`Waiting for volume detach to complete` line.

### Exact Divergence Point

The exact divergence is the return from:

```go
volumeattach.Delete(ctx, t.ClientSet.Compute, instanceUUID, volume.ID).ExtractErr()
```

in `internal/target.OpenStack.Disconnect`.

In successful detach attempts, this call returns nil and execution continues
into `waitForVolumeDetached`. In failing attempts, this call returns the Nova
HTTP 400 error and the wait loop never starts.

### Responsible Functions

- `internal/vmware_nbdkit.NbdkitServers.MigrationCycle` controls per-disk
  sequencing. It is serial: the next disk starts only after
  `SyncToTarget` returns.
- `internal/vmware_nbdkit.NbdkitServer.SyncToTarget` calls `t.Connect`, runs
  the copy, and defers `t.Disconnect`.
- `internal/target.OpenStack.Connect` creates the Nova volume attachment and
  waits only for local device discovery before returning.
- `internal/target.OpenStack.Disconnect` issues the Nova detach DELETE.
- `internal/target.OpenStack.waitForVolumeDetached` confirms detach only after
  Nova accepts DELETE.

### Why One Path Continues And The Other Fails

The successful 7-disk detach attempts have enough elapsed time between local
device discovery and detach for OpenStack to finish attachment state
transitions. Nova accepts DELETE, so detach polling can begin and complete.

The failing attempts have a much shorter copy window. The local block device is
visible, so `Connect` returns and copy begins, but the OpenStack control plane
does not yet consider the volume fully `in-use` / attached by the time
`Disconnect` sends DELETE. Nova refuses the detach because its state validation
requires the volume to be attached before it can be detached.

This makes the failure timing-sensitive: larger or slower copies naturally hide
the race, while fast incremental copies expose it.

## Evidence

Log evidence:

- 2-disk log:
  - line 10: local device found for the first volume;
  - line 11: first disk copy starts;
  - line 13: disconnect begins for disk `2000`;
  - line 14: detach DELETE is attempted;
  - line 15: Nova rejects DELETE with HTTP 400 because volume status /
    attach-status is not detachable;
  - no `Waiting for volume detach to complete` line appears.
- 7-disk log, successful disk `2000`:
  - line 19: detach DELETE is attempted;
  - line 20: wait loop starts, proving DELETE was accepted;
  - lines 21-22: polling sees in-progress detach state;
  - line 23: detach completion is confirmed;
  - line 25: the next disk begins.
- 7-disk log, successful disk `2001`:
  - line 34: detach DELETE is attempted;
  - line 35: wait loop starts;
  - line 38: detach completion is confirmed.
- 7-disk log, failing disk `2017`:
  - line 45: local device found;
  - line 47: copy is effectively immediate;
  - line 49: detach DELETE is attempted;
  - line 50: Nova rejects DELETE with the same HTTP 400;
  - no wait-loop line appears.

Code evidence:

- `OpenStack.Connect` waits for `findDevice(volume.ID)` to return a device path,
  but does not wait for Cinder volume status `in-use`, Cinder attachments, or
  Nova server volume attachments to show the attachment as complete.
- `OpenStack.Connect` discards the attachment object returned by
  `volumeattach.Create`.
- `OpenStack.Disconnect` refetches the Cinder volume once before DELETE, but it
  does not wait for attach-ready state before attempting DELETE.
- `waitForVolumeDetached` is reached only after Nova accepts DELETE, so it
  cannot handle the pre-DELETE race shown in the failing logs.
- `MigrationCycle` and `SyncToTarget` preserve serial per-disk sequencing; the
  logs show the process is not concurrently processing the next disk before the
  current disk's disconnect finishes or fails.

## Confidence Levels

Ranked suspected causes:

1. **Attachment/volume state handling: High**
   - `Connect` treats local device discovery as attach completion.
   - Nova's error says the volume is not in the attached state required for
     detach.
   - Successful cases have longer copy windows before detach.
2. **Race condition / asynchronous OpenStack behavior: High**
   - The same code succeeds when more time passes and fails when copy completes
     almost immediately.
   - Local device discovery and control-plane attachment state are not the same
     readiness signal.
3. **Timeout/retry logic: Medium**
   - There is no retry or wait around a detach DELETE rejected because the
     volume is not yet detachable.
   - A small bounded retry around this specific state transition could mask the
     race, but attach readiness should be fixed first.
4. **Stale attachment IDs: Medium-Low**
   - The code discards the `volumeattach.Create` result and uses `volume.ID` for
     DELETE.
   - However, successful detaches use the same DELETE shape, so this is not the
     best explanation for the observed divergence.
5. **Incorrect polling logic: Low for this failure**
   - The failing path never reaches `waitForVolumeDetached`.
   - Polling may still need more observability, but it is not the first
     divergence.
6. **Per-disk state tracking: Low**
   - Disk keys and volume IDs differ correctly in the logs.
   - The sequence is serial and volume lookup appears per-disk.
7. **Migration loop sequencing: Low**
   - `MigrationCycle` waits for `SyncToTarget` to return before moving to the
     next disk.
8. **virt-v2v cleanup: Low**
   - The failing logs are `migrate` runs, not final cutover `virt-v2v` paths.

## Proposed Fix

Smallest safe change: make `OpenStack.Connect` wait for OpenStack attach
readiness before returning to the copy path.

Specifically, after `volumeattach.Create` and local device discovery, keep
polling the exact Cinder volume until it reports an attached state compatible
with a future detach:

- Cinder volume status is `in-use`;
- Cinder attachment count is greater than zero;
- one attachment matches the current migration worker instance UUID when that
  field is available;
- the local device path for the same volume is present.

For the already-attached path where `GetPath` returns a device before
`volumeattach.Create` is called, run the same readiness check before returning.

Keep the current post-DELETE `waitForVolumeDetached` behavior. It is useful once
Nova accepts DELETE; the missing piece is the pre-copy / pre-detach attach
readiness gate.

Optional but still small follow-up: when `volumeattach.Delete` returns the
specific OpenStack "not attached / not in-use" 400, refetch the volume state and
log it. Retrying DELETE should be considered only if the retry condition is
tightly scoped and does not hide unrelated OpenStack failures.

## Regression Risks

- The migration may wait longer before copying each disk, especially on clouds
  where Cinder/Nova state transitions lag behind local device creation.
- Some OpenStack backends may expose attachment details differently in the
  Gophercloud `volumes.Volume.Attachments` structure. The readiness check should
  be conservative: if instance matching is unavailable, require at least
  `status=in-use`, attachment count greater than zero, and local device present.
- Existing already-attached/stale local device cases may now fail or wait
  instead of proceeding. That is safer for correctness, but it may expose
  cleanup problems that were previously hidden.
- If a backend reports a nonstandard status while the device is genuinely usable,
  the new wait could time out. Logging the observed status and attachments will
  be important for tuning.
- Retrying DELETE too broadly would be risky because real OpenStack errors must
  not be masked as success.

## Recommended Tests

Unit-level tests:

- Attach readiness helper test:
  - returns false for `available` with zero attachments even when a local device
    exists;
  - returns true for `in-use` with an attachment and matching local device;
  - returns false for `in-use` without a local device.
- `Connect` wait test:
  - simulate local device appearing before Cinder status becomes `in-use`;
    assert `Connect` does not return until both are true.
- `Connect` timeout test:
  - simulate local device present but Cinder never leaving `available`; assert a
    useful timeout error.
- DELETE rejection diagnostic test:
  - simulate `volumeattach.Delete` returning the specific HTTP 400 and assert
    the error is returned and state logging/refetch is attempted if implemented.
- Multi-disk sequencing test:
  - disk 2 `Connect` must not start until disk 1 `Disconnect` has completed or
    returned an error.
- Existing detach wait tests:
  - keep coverage that `Disconnect` waits until Cinder status is `available`,
    attachment count is zero, and local device path is gone.

Integration/manual tests:

- Run a multi-disk incremental migration where at least one disk has no changed
  blocks or an extremely small changed set.
- Confirm logs show attach readiness before copy starts.
- Confirm fast-copy disks detach successfully without requiring manual delay.
- Repeat with a slow OpenStack backend and verify attach and detach timeout
  errors include volume status, attachment count, instance UUID, volume ID, disk
  key, and local device path.

## Additional Logging To Validate The Fix

Add structured debug/info logging around these points:

- Immediately after `volumeattach.Create`:
  - disk key,
  - volume ID,
  - current migration worker instance UUID,
  - returned attachment ID if available,
  - returned device name if available.
- During attach readiness polling:
  - Cinder volume status,
  - Cinder attachment count,
  - attachment server IDs if exposed,
  - local device path,
  - elapsed wait time.
- Immediately before copy starts:
  - explicit "volume attach ready" log with disk key, volume ID, status,
    attachment count, and local path.
- Immediately before `volumeattach.Delete`:
  - disk key,
  - volume ID,
  - current Cinder status,
  - attachment count,
  - local device path.
- When `volumeattach.Delete` fails:
  - original Nova error,
  - refetched Cinder status and attachments,
  - local device path.
- During detach polling:
  - keep the existing status, attachment count, and local device fields.
  - add elapsed wait time if possible.

Avoid logging VMware or OpenStack credentials. The provided debug logs include a
password in the nbdkit command line, so future debug logging should redact
secrets before printing external commands.

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
