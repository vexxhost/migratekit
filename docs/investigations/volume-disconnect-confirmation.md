# Volume Disconnect Confirmation Investigation

## Problem Summary

During target disk cleanup, migratekit can report a failed OpenStack volume
disconnect even when the volume detach is progressing normally and later appears
detached in OpenStack.

The representative failure is:

```text
time="2026-07-22T00:53:07Z" level=info msg="Disconnecting target disk" disk_key=2001
time="2026-07-22T00:53:07Z" level=info msg="Detaching volume" device=/dev/vde disk_key=2001 volume_id=cb562d94-3cd8-42b2-abf6-7e846a639df8
time="2026-07-22T00:53:08Z" level=info msg="Waiting for volume detach to complete" disk_key=2001 volume_id=cb562d94-3cd8-42b2-abf6-7e846a639df8
time="2026-07-22T00:53:16Z" level=error msg="Failed to disconnect target disk" disk_key=2001 error="lstat /dev/vde: no such file or directory"
Error: lstat /dev/vde: no such file or directory
```

Highest-confidence diagnosis: `filepath.EvalSymlinks` inside
`internal/target.findDevice` is following a matching `/dev/disk/by-id` symlink
while udev or the kernel has already removed the underlying device node. The
resulting `os.ErrNotExist` is returned as a hard error, even though local device
disappearance is one of the expected signals that detach is completing.

## Impact on Migrate

`migratekit migrate` runs one shared `MigrationCycle`. Each disk is processed
serially. If `SyncToTarget` returns a disconnect error for a disk,
`MigrationCycle` returns that error to the command and the CLI exits non-zero.

The source VMware VM remains powered on during `migrate`, so the operational
impact is usually recoverable: after OpenStack eventually finishes detach, the
operator can rerun a migration cycle. The current source code does not contain a
per-disk continue-on-disconnect-error path. If logs appear to show processing
continuing, that is likely from an outer wrapper, a separate rerun, or a log
slice where a later disk belongs to a different cycle.

## Impact on Cutover

`migratekit cutover` uses the same disk disconnect path in two places:

- the online migration cycle before source shutdown;
- the final migration cycle after source shutdown and before destination Nova
  server creation.

A false disconnect failure in the online cycle aborts cutover before shutdown. A
false disconnect failure in the final cycle is more disruptive: the VMware
source may already be powered off, and `CreateResourcesForVirtualMachine` is not
called because the final `MigrationCycle` returned an error.

## Reproduction Scenario

This investigation did not reproduce the bug against live OpenStack. A likely
reproduction is:

1. Run `migratekit migrate` or `migratekit cutover` from an OpenStack helper
   instance with `/dev` and `/dev/disk/by-id` visible to the process.
2. Copy a disk whose Cinder volume is attached to the helper instance.
3. Let migratekit request a Nova volume detach.
4. Under OpenStack or host-device load, allow the local block device node to
   disappear while a stale or in-flight `/dev/disk/by-id` symlink is still
   visible.
5. Observe `waitForVolumeDetached` exit early with `lstat /dev/<device>: no
   such file or directory`.

## Expected Behavior

After a disk copy finishes, migratekit should:

1. request detach of the target Cinder volume from the migration helper
   instance;
2. poll OpenStack until the helper attachment is gone and the volume is detached;
3. poll local device state until the helper host no longer exposes the block
   device;
4. treat expected device disappearance as detach progress, not as an immediate
   failure;
5. return real OpenStack API errors, permission errors, and detach timeouts.

## Actual Behavior

The current code starts the detach wait, but each poll calls `findDevice`.
`findDevice` returns any `filepath.EvalSymlinks` error directly. If a matching
by-id symlink resolves to a device node that disappeared during detach,
`EvalSymlinks` can return `lstat /dev/vde: no such file or directory`.

That error exits the detach waiter immediately. The configured detach timeout is
not reached, and the normal success condition is never evaluated for that poll.

## Relevant Logs

The sample log shows the important sequence:

- `00:53:07`: `SyncToTarget` begins deferred cleanup with
  `Disconnecting target disk`.
- `00:53:07`: `OpenStack.Disconnect` finds a local path and logs
  `Detaching volume`.
- `00:53:08`: Nova accepted the detach request, because
  `waitForVolumeDetached` starts and logs `Waiting for volume detach to
  complete`.
- `00:53:16`: the waiter returns `lstat /dev/vde: no such file or directory`.

The failure is therefore not the earlier multi-disk race where Nova rejects the
detach request before polling begins. Here, polling begins successfully and then
is interrupted by local device path resolution.

## Relevant Files and Functions

- `main.go`
  - `migrateCmd.RunE` calls `servers.MigrationCycle(ctx, false)`.
  - `cutoverCmd.RunE` calls `MigrationCycle` once online and once after source
    shutdown.
- `internal/vmware_nbdkit/vmware_nbdkit.go`
  - `NbdkitServers.MigrationCycle` loops over disks serially.
  - `NbdkitServer.SyncToTarget` logs `Disconnecting target disk` in a deferred
    cleanup function and propagates disconnect errors.
- `internal/target/openstack.go`
  - `OpenStack.Disconnect` logs `Detaching volume`, calls Nova
    `volumeattach.Delete`, then calls `waitForVolumeDetached`.
  - `waitForVolumeDetached` logs `Waiting for volume detach to complete`.
  - `waitForVolumeDetached` polls Cinder and local device state once per
    second for up to five minutes.
  - `findDevice` calls `os.ReadDir("/dev/disk/by-id/")`, matches names
    containing the first 18 characters of the volume UUID, and calls
    `filepath.EvalSymlinks` on the matching by-id entry.
- `docs/investigations/multidisk-detach-timeout.md`
  - The previous investigation covers a different race: attach readiness before
    Nova accepts detach.

## Migrate Execution Path

1. `migrateCmd.RunE` creates `NbdkitServers`.
2. `MigrationCycle` starts nbdkit servers and creates a VMware snapshot.
3. For each VMware disk, `MigrationCycle` creates an OpenStack target.
4. `SyncToTarget` connects the target, copies data, writes change ID metadata,
   and defers `t.Disconnect(ctx)`.
5. The deferred cleanup logs `Disconnecting target disk`.
6. `OpenStack.Disconnect` requests Nova detach and starts detach polling.
7. If `findDevice` returns `lstat /dev/vde`, `Disconnect` returns the error.
8. `SyncToTarget` logs `Failed to disconnect target disk` and returns the
   disconnect error.
9. `MigrationCycle` returns that error to `migrateCmd.RunE`.

## Cutover Execution Path

1. `cutoverCmd.RunE` validates flavor and ensures Neutron ports.
2. It runs an online `MigrationCycle(ctx, false)`.
3. It shuts down the source VMware VM if needed.
4. It runs a final `MigrationCycle(ctx, enablev2v)`.
5. It creates the destination Nova server only after the final cycle returns.

The disconnect path inside each cutover cycle is identical to `migrate`. The
difference is operational context: cutover may have already shut down the source
VM before a false final-cycle disconnect error prevents destination server
creation.

## OpenStack Detach State Model

Volume status alone should not be treated as fully authoritative. Cinder volume
status, Cinder attachment records, Nova server volume attachments, and local
device state can lag or lead each other during attach and detach.

The safest detach confirmation for this tool is:

- the helper server's volume attachment no longer exists;
- the volume no longer lists the helper server attachment;
- for this non-multiattach flow, the volume status is `available`;
- the helper host no longer exposes a local block device for the volume.

The current implementation approximates this by requiring Cinder status
`available`, zero Cinder attachments, and no local device path. That is
reasonable for the current single-helper attachment model, but exact attachment
ID tracking would be stronger if the target ever supports multiattach or stale
attachment cleanup.

## Local Device Removal State Model

Local block device disappearance is expected during detach. The local state can
move through several transient forms:

- the concrete device node exists, for example `/dev/vde`;
- `/dev/disk/by-id` contains a symlink that resolves to the concrete node;
- the concrete node is removed while the by-id symlink is still visible;
- the by-id symlink is removed;
- no local path remains for the volume.

The third state is the likely failing state. `os.ReadDir("/dev/disk/by-id/")`
can observe a matching symlink, but `filepath.EvalSymlinks` can fail because the
symlink target has disappeared. In detach polling, that should be interpreted as
"no usable local device path for this volume at this instant" unless the error
is something other than missing path.

## Error Propagation Analysis

The `lstat` error originates in `internal/target.findDevice` at the
`filepath.EvalSymlinks(filepath.Join("/dev/disk/by-id/", file.Name()))` call.

The call chain is:

```text
NbdkitServer.SyncToTarget deferred cleanup
  -> OpenStack.Disconnect
     -> waitForVolumeDetached
        -> findDevice
           -> filepath.EvalSymlinks
              -> lstat /dev/vde
```

`findDevice` calls `lstat` indirectly because `EvalSymlinks` resolves a
matching `/dev/disk/by-id` symlink to the concrete block device path. The
returned path is used to decide whether the local device is still present.

Before this fix, `waitForVolumeDetached` returned the `findDevice` error
immediately. `OpenStack.Disconnect` returned it to `SyncToTarget`. The deferred
cleanup in `SyncToTarget` recorded it as the function return value when the copy
otherwise succeeded. `MigrationCycle` then returned it to either `migrate` or
`cutover`.

Before this fix, the code did not handle `os.IsNotExist`,
`errors.Is(err, os.ErrNotExist)`, or an equivalent missing-path check for this
`EvalSymlinks` failure.

## Timing and Retry Analysis

The sample log shows about one second between detach request and the start of
the wait, then about eight seconds before failure. That is not an intentional
eight-second detach timeout.

Current source constants are:

- attach timeout: two minutes;
- attach poll interval: one second;
- detach timeout: five minutes;
- detach poll interval: one second.

The configured detach timeout is not honored in the failing path because an
inner filesystem check returns an error before the polling loop reaches its
timeout branch. Increasing the detach timeout alone would not solve this
failure: the first stale-symlink `os.ErrNotExist` could still abort the waiter
immediately.

Bounded polling is already present. The safer change is to classify missing
local device targets correctly and let the existing bounded loop continue until
OpenStack and local states agree, or until the configured timeout expires.

## Root Cause

`findDevice` treats `os.ErrNotExist` from symlink target resolution as a hard
device lookup failure. During detach, the same condition is an expected local
device removal transition.

Because `waitForVolumeDetached` depends on `findDevice`, an expected local
device disappearance can abort detach confirmation before migratekit observes
the final OpenStack state. This produces a false negative: OpenStack completes
the detach, but migratekit exits non-zero first.

## Evidence

- The logs prove Nova accepted the detach request because
  `Waiting for volume detach to complete` is emitted after
  `volumeattach.Delete`.
- The error text is a local filesystem error, not a Cinder or Nova API error.
- The failing delay is far below the five-minute detach timeout.
- `findDevice` is the only disconnect path that resolves `/dev/disk/by-id`
  entries and can produce an `lstat /dev/<device>` error.
- `volumeDetachComplete` already defines missing local path as part of success,
  but the missing-path error prevents the code from passing `devicePath == ""`
  into that predicate.
- The previous multi-disk detach investigation and the current upstream PR #163
  address attach readiness before detach. They do not address
  `EvalSymlinks` returning `os.ErrNotExist` during detach polling.
- A live upstream check on 2026-07-24 showed PRs #155, #163, and #164 still
  open. Relevant attachment PRs do not appear to handle this exact stale local
  device path case.

## Confidence Level

Confidence is high.

The code path, log sequence, and error string align tightly. Alternative causes
are less likely:

- insufficient timeout: low confidence, because the configured timeout is five
  minutes and the failure occurs after about eight seconds;
- wrong success condition: medium-low confidence, because the current success
  condition is conservative, but the loop exits before evaluating it;
- stale attachment ID: low confidence for this specific failure, because Nova
  detach was already accepted;
- migrate/cutover-specific polling difference: low confidence, because both
  commands share the same `SyncToTarget` and `OpenStack.Disconnect` path;
- real OpenStack API failure: low confidence for this log, because the final
  error is local `lstat`, not HTTP/API state.

## Implementation Summary

Implemented the smallest safe fix in `internal/target/openstack.go`.

The change does not increase the detach timeout and does not add broad retries
around `volumeattach.Delete`. Instead, it corrects local device-state
classification and keeps the existing bounded detach poll.

Implemented behavior:

- `findDevice` now delegates to an unexported directory-scoped helper so tests
  can exercise `/dev/disk/by-id` behavior without mutating the real host `/dev`.
- `errors.Is(err, os.ErrNotExist)` from `filepath.EvalSymlinks` is treated as
  no usable local device path for that matching by-id entry, and lookup
  continues.
- Other filesystem errors, such as symlink loops, still return as errors.
- Attach and detach polling now receive `("", nil)` for expected device
  disappearance rather than a fatal `lstat` error.
- Detach polling still requires Cinder status `available`, zero Cinder
  attachments, and no local device path before success.
- Detach polling now also tracks whether the helper server attachment remains
  and records helper attachment IDs when Cinder exposes them.
- If the detach wait expires, the returned timeout includes the last observed
  status, attachment count, helper attachment state, helper attachment IDs,
  device path, and elapsed wait time.
- If the poll context expires during a Cinder refresh, the waiter returns the
  same final-state timeout shape instead of a raw context deadline error.
- Real OpenStack API errors and real detach request failures are still returned
  immediately.

The production wrapper still reads `/dev/disk/by-id/`, and public CLI behavior
is unchanged.

## Regression Risks

- Treating every symlink resolution failure as disappearance would be too broad.
  The implementation should only downgrade missing-path errors.
- If stale by-id entries linger indefinitely while OpenStack remains attached,
  the detach waiter should still time out and report the last OpenStack state.
- Returning `("", nil)` from device lookup is correct for attach/detach polling,
  but copy code should still avoid writing to an empty or missing path.
- Clouds with unusual Cinder attachment semantics may need exact Nova
  attachment-list checks or attachment ID tracking in a later fix.
- Extra debug logging around `/dev/disk/by-id` can become noisy if emitted every
  second for many disks.

## Tests Added

Added focused unit tests in `internal/target/openstack_test.go`:

- device exists and resolves through a matching by-id symlink;
- matching by-id symlink target is missing and returns no device path instead
  of an error;
- unexpected filesystem errors still return errors;
- local device disappears immediately after detach;
- local device disappears before OpenStack attachment state clears;
- OpenStack attachment state clears before the local device disappears;
- helper attachment remains until timeout;
- OpenStack returns a real API error while refreshing detach state;
- Nova detach request returns a real API error;
- sequential detach waits for multiple volume IDs do not retain stale device
  state.

The existing `internal/vmware_nbdkit` test continues to cover disconnect error
propagation through `SyncToTarget`, which is the shared path used by both
`migrate` and `cutover`.

## Integration Test Plan

1. Run a multi-disk `migratekit migrate` against OpenStack under normal load.
2. Repeat during known busy periods or while detaches are slow.
3. Watch:
   - `openstack server volume list <helper-server-id>`;
   - `openstack volume show <volume-id>`;
   - `ls -l /dev/disk/by-id/`;
   - `ls -l /dev/<device>`.
4. Confirm that local device disappearance before final Cinder status does not
   abort the waiter.
5. Confirm that real Nova/Cinder API errors still abort immediately.
6. Repeat `migratekit cutover` and verify the final cycle detaches every volume
   before destination server creation.

## Logging Improvements

Safe logging now helps distinguish expected local disappearance from real detach
failures without exposing secrets:

- detach request logs include disk key, volume ID, helper instance UUID, local
  device path, and helper attachment IDs when available;
- detach polling logs status, attachment count, helper attachment state, helper
  attachment IDs, device path, and elapsed wait time;
- successful detach logs the final observed state;
- timeout errors and timeout logs include the last observed Cinder status,
  attachment count, helper attachment state, helper attachment IDs, device path,
  and elapsed wait time;
- do not log OpenStack tokens, passwords, environment variables, clouds.yaml
  contents, or VMware credentials.

## Validation Performed

Validation performed in the Fedora development container from `AGENTS.md`:

- `GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go fmt ./...`
- `GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test ./internal/target`
- `GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test ./internal/vmware_nbdkit`
- `GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go vet ./...`
- `GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test ./...`
- `git diff --check`

## Remaining Runtime Validation

Live VMware/OpenStack integration testing has not been performed in this
investigation. The remaining runtime validation is:

- run `migratekit migrate` against a multi-disk VM and confirm local device
  disappearance during detach does not produce a fatal `lstat` error;
- run `migratekit cutover` and confirm the final cycle detaches every migrated
  volume before destination Nova server creation;
- confirm real Cinder/Nova detach failures still abort with useful diagnostics;
- confirm the helper server no longer lists the migrated volume after each
  detach.

## Manual Validation Plan

1. Start the required Fedora Docker development container from `AGENTS.md`.
2. Install `nbdkit`, `nbdkit-vddk-plugin`, `libnbd`, `libnbd-devel`, `golang`,
   and `virt-v2v`.
3. Confirm VDDK is mounted at `/usr/lib64/vmware-vix-disklib/`.
4. Run `migratekit migrate --debug ...` for a VM with multiple disks.
5. During detach, run:

   ```bash
   openstack server volume list <helper-server-id>
   openstack volume show <volume-id>
   ls -l /dev/disk/by-id/
   ls -l /dev/<device>
   ```

6. Verify that expected disappearance of `/dev/<device>` does not produce a
   fatal `lstat` error.
7. Repeat with `migratekit cutover --debug ...`.
8. Before destination server creation, verify each migrated volume is detached
   from the helper server and no local helper device remains.

## Compatibility Classification

The implemented fix is upstream-compatible. It corrects local device-state
classification during OpenStack detach polling without changing CLI flags,
resource ownership, migration ordering, or credential handling.
