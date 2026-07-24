# Fork History

## 2026-07-24 - OpenStack Volume Disconnect Confirmation

### Summary

Fixed intermittent false failures during OpenStack target-volume disconnect when
the local block device disappears during detach confirmation.

### Root Cause

`internal/target.findDevice` treated `os.ErrNotExist` from resolving a matching
`/dev/disk/by-id` symlink as a hard failure. During detach, that missing target
is expected because the local block device can disappear before the by-id entry
is fully removed.

### Solution

Treat missing symlink targets as an absent local device path while preserving
real filesystem errors. Detach confirmation still uses bounded polling and now
logs helper attachment state, attachment IDs when available, device path,
elapsed wait time, and final success/failure state.

### Validation

- `go fmt ./...`
- `go test ./internal/target`
- `go test ./internal/vmware_nbdkit`
- `go vet ./...`
- `go test ./...`
- `git diff --check`

### Compatibility

Upstream-compatible.

### Related Files

- `internal/target/openstack.go`
- `internal/target/openstack_test.go`
- `docs/investigations/volume-disconnect-confirmation.md`

## 2026-07-05 - OpenStack Token Refresh for Long Disk Copies

### Summary

Enabled Gophercloud reauthentication so long-running disk migrations can refresh
OpenStack credentials after Keystone token expiry and retry a 401 response once.

### Root Cause

Migratekit authenticated OpenStack clients once per target disk, then reused the
same provider across long copy operations. Gophercloud reauthentication was not
enabled, so post-copy Cinder/Nova calls could reuse an expired token.

### Solution

Enable reusable OpenStack auth reauthentication in `internal/openstack`, add
safe reauth diagnostics with operation labels, and preserve token-only auth
behavior.

### Validation

- `go fmt ./...`
- `go vet ./...`
- `go test ./...`

### Compatibility

Upstream-compatible.

### Related Files

- `internal/openstack/client.go`
- `internal/openstack/client_test.go`
- `internal/target/openstack.go`
- `main.go`
- `docs/investigations/openstack-token-expiry-long-copy.md`

## 2026-06-28 - Upstream PR #154 VDDK Environment Scoping

### Summary

Integrated upstream PR [#154](https://github.com/vexxhost/migratekit/pull/154)
to scope the VMware VDDK library path to the `nbdkit` child process.

### Root Cause

`internal/nbdkit.NbdkitBuilder.Build` used `os.Setenv` to set
`LD_LIBRARY_PATH=/usr/lib64/vmware-vix-disklib/lib64` globally in the migratekit
process. That environment value could leak into later child processes such as
`virt-v2v-in-place` and `supermin`.

### Solution

Set `cmd.Env` on the `nbdkit` `exec.Cmd` instead of mutating the parent process
environment.

### Validation

- `go fmt ./...`
- `go vet ./...`
- `go test ./...`

### Compatibility

Upstream-compatible.

### Related Files

- `internal/nbdkit/builder.go`
- `docs/upstream-sync.md`

### Related Upstream PR

- <https://github.com/vexxhost/migratekit/pull/154>
