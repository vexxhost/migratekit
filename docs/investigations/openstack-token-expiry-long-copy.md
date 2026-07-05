# OpenStack Token Expiry During Long Disk Copy

Date: 2026-07-05

## Problem Summary

Large disk migrations can run long enough for the OpenStack Keystone token to
expire. A reported migration copied a large disk for about 14 hours and then
failed while disconnecting the target Cinder volume with a 401 Unauthorized
response from the volume API.

## Evidence

- The failure occurred after the copy completed, when migratekit resumed
  OpenStack API calls for target disk cleanup.
- `target.OpenStack.Disconnect` first calls
  `openstack.ClientSet.GetVolumeForDisk`, which lists Cinder volumes using the
  same service client created before the copy started.
- `vmware_nbdkit.NbdkitServer.SyncToTarget` can run long full-copy,
  incremental-copy, and optional `virt-v2v-in-place` work between OpenStack API
  calls.
- `internal/openstack.NewClientSet` authenticated once with
  `openstack.Authenticate`, but did not enable Gophercloud reauthentication.
  Gophercloud only installs its bounded 401 reauth retry when
  `AuthOptions.AllowReauth` is true.
- Upstream PR review on 2026-07-05 found no existing token refresh fix.
  PR #163 touches OpenStack attach/detach waits but has no auth, token, 401, or
  reauth changes. PR #124 only updates the Gophercloud dependency.

## Root Cause

The OpenStack provider and service clients are created once per target disk and
then reused across the disk copy. Since `AllowReauth` was left at its default
false value, the provider had no `ReauthFunc`. When Keystone expired the token
during a long copy, the next Cinder/Nova request reused the stale token and
returned 401 instead of refreshing credentials and retrying once.

This was not caused by a migratekit context timeout. It was also not caused by a
Cinder detach timeout; the failing request was the pre-detach volume lookup.

## Long-Running Windows

OpenStack API calls can happen after many hours of little or no OpenStack API
activity in these paths:

- Full copy: `SyncToTarget` calls `FullCopyToTarget`, then `WriteChangeID`, then
  deferred `Disconnect`.
- Incremental copy: `SyncToTarget` calls `IncrementalCopyToTarget`, then
  `WriteChangeID`, then deferred `Disconnect`.
- Cutover with conversion: `virt-v2v-in-place` can add more time before
  `WriteChangeID` and `Disconnect`.
- Multi-disk migrations repeat the same sequence per disk with a distinct
  target client set for each disk.

## Resolution

- Enabled Gophercloud reauthentication for reusable auth options in
  `internal/openstack.NewClientSet`.
- Preserved token-only behavior by not enabling reauth when a token ID is
  already the only available credential.
- Wrapped the Gophercloud provider `ReauthFunc` to log safe diagnostics:
  attempt, success/failure, and the operation label. Tokens, passwords,
  clouds.yaml contents, and environment variables are not logged.
- Added operation labels around OpenStack API calls so a 401-triggered reauth
  log indicates which operation needed a fresh token.
- Relied on Gophercloud's existing one-reauth-per-request behavior to avoid
  endless 401 retries.

## Files and Functions Changed

- `internal/openstack/client.go`
  - `NewClientSet`
  - `enableReauthentication`
  - `wrapReauthLogging`
  - `WithReauthOperation`
  - default operation labels for `ClientSet` methods
- `internal/target/openstack.go`
  - operation labels for target volume create, attach, wait, detach, lookup,
    and change-ID update calls
- `main.go`
  - operation label for cutover flavor validation
- `internal/openstack/client_test.go`
  - 401 reauth retry coverage and no-endless-retry coverage

## Validation Performed

All validation was run inside the Fedora 40 development container.

The container `golang` package provided Go 1.23.8, while this repository
requires Go 1.25.0. Validation used Go's toolchain auto-download with
`GOTOOLCHAIN=auto GOSUMDB=sum.golang.org`.

Commands:

```bash
go fmt ./...
go vet ./...
go test ./...
```

Results:

- `go fmt ./...`: passed
- `go vet ./...`: passed
- `go test ./...`: passed
- Focused OpenStack tests confirmed that a 401 response triggers exactly one
  reauthentication and retries the request with a fresh token.
- Focused OpenStack tests confirmed that repeated 401 responses are not retried
  endlessly.

## Skipped Validation

Live VMware/OpenStack integration validation was not performed. The environment
has the VDDK mount path available for the container, but this session does not
include a real VMware VM, OpenStack project credentials, Cinder/Nova endpoint,
or a large disk migration target.

## Manual Validation Steps

1. Use a non-production OpenStack cloud or maintenance window.
2. Source a normal password-based or application-credential OpenStack auth
   environment.
3. Run a migration whose full copy lasts longer than the Keystone token lifetime
   or temporarily lower token lifetime in a test cloud.
4. Confirm the copy completes and migratekit proceeds through `WriteChangeID`
   and `Disconnect` without manual restart.
5. Confirm logs include safe messages like `Attempting OpenStack
   reauthentication` and `OpenStack reauthentication succeeded` with an
   operation label, and do not include tokens, passwords, clouds.yaml contents,
   or environment variables.
6. Confirm the target volume is detached from the migratekit worker after copy.
7. Repeat with multiple disks and with the cutover flow, including
   `virt-v2v-in-place` when applicable.

## Remaining Risks

- Live Keystone reauth behavior was not exercised against a real OpenStack
  deployment in this session.
- Token-only auth cannot be refreshed because there are no reusable credentials;
  this behavior is preserved intentionally.
- If reauthentication fails because credentials are revoked or the project scope
  is no longer valid, migratekit returns the real failure instead of masking it.
- Long migrations can still fail for non-auth reasons such as source read
  errors, Cinder attachment failures, or context cancellation.

## Lessons Learned

OpenStack client setup must opt into Gophercloud reauthentication. Long copy
operations create a natural token-expiry boundary, so the provider session
should be responsible for refreshing credentials instead of each attach/detach
call site handling auth failures independently.

## Future Regression Tests

- Add an integration test against a Keystone/Cinder test environment with a
  deliberately short token lifetime.
- Add a multi-disk migration test that forces token expiry before the second
  disk's post-copy cleanup.
- Add a cutover test with `virt-v2v-in-place` duration long enough to cross a
  token boundary.
