# Fork History

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
