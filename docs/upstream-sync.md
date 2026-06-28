# Upstream Synchronization

## Current Status

- Review date: 2026-06-28
- Upstream repository: <https://github.com/vexxhost/migratekit>
- Number of PRs reviewed: 17
- Current synchronization status: PR #154 has been integrated locally on branch
  `upstream/pr-154`. Remaining upstream PRs are pending approval and should
  proceed one PR at a time.

## Synchronization Strategy

Follow the upstream workflow from `AGENTS.md`:

- Never work directly on `main`.
- Never commit directly to `main`.
- Never push directly to `main`.
- Always start integration work from the latest `main`.
- Integrate one upstream PR at a time.
- Use branches named `upstream/pr-<number>`.
- Validate each integration independently.
- Merge through Pull Requests only.
- Update this document after every completed upstream integration.
- Update `docs/fork-history.md` when an upstream merge materially changes this
  fork.

Recommended branch workflow:

```bash
git checkout main
git pull --ff-only origin main
git checkout -b upstream/pr-<number>
```

Required validation before an integration is considered complete:

```bash
go fmt ./...
go vet ./...
go test ./...
```

If integration tests cannot be executed, record exactly why and list the manual
validation still required.

## Upstream PR Tracking

| PR | Title | Status | Decision | Planned Branch | Priority | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| [#154](https://github.com/vexxhost/migratekit/pull/154) | fix(nbdkit): scope VDDK library path to nbdkit | Integrated | Merge Immediately | `upstream/pr-154` | P0 | Integrated locally 2026-06-28; pending fork PR/merge. |
| [#141](https://github.com/vexxhost/migratekit/pull/141) | ci: enforce least-privilege permissions for GitHub Actions workflows | Ready to Integrate | Merge Immediately | `upstream/pr-141` | P0 | Low-risk CI permission hardening; verify GHCR publish permissions. |
| [#132](https://github.com/vexxhost/migratekit/pull/132) | fix(deps): update module github.com/sirupsen/logrus to v1.9.4 | Pending Review | Merge After Review | `upstream/pr-132` | P1 | Low-risk logging dependency bump. |
| [#143](https://github.com/vexxhost/migratekit/pull/143) | fix(deps): update module github.com/spf13/cobra to v1.10.2 | Pending Review | Merge After Review | `upstream/pr-143` | P1 | Validate CLI help and flag parsing. |
| [#130](https://github.com/vexxhost/migratekit/pull/130) | fix(deps): update module github.com/schollz/progressbar/v3 to v3.19.0 | Pending Review | Merge After Review | `upstream/pr-130` | P1 | Validate copy progress output. |
| [#146](https://github.com/vexxhost/migratekit/pull/146) | fix(deps): update module github.com/vmware/govmomi to v0.55.0 | Pending Review | Merge After Review | `upstream/pr-146` | P2 | Requires VMware login, snapshot, and CBT validation. |
| [#131](https://github.com/vexxhost/migratekit/pull/131) | fix(deps): update module github.com/thediveo/enumflag/v2 to v2.2.1 | Pending Review | Merge After Review | `upstream/pr-131` | P2 | Validate enum-backed flags. |
| [#145](https://github.com/vexxhost/migratekit/pull/145) | fix(deps): update module github.com/erikgeiser/promptkit to v0.11.0 | Pending Review | Merge After Review | `upstream/pr-145` | P2 | Validate interactive snapshot prompt. |
| [#147](https://github.com/vexxhost/migratekit/pull/147) | fix(deps): update module libguestfs.org/libnbd to v1.25.4 | Pending Review | Merge After Review | `upstream/pr-147` | P2 | Validate incremental-copy data path. |
| [#124](https://github.com/vexxhost/migratekit/pull/124) | fix(deps): update module github.com/gophercloud/gophercloud/v2 to v2.13.0 | Pending Review | Merge After Review | `upstream/pr-124` | P2 | Validate OpenStack volume, server, network, attach, and detach flows. |
| [#134](https://github.com/vexxhost/migratekit/pull/134) | Closes Issue #133 - removes required vmware-password argument | Pending Review | Merge After Review | `upstream/pr-134` | P3 | Needs cleanup and interactive/non-interactive CLI validation. |
| [#155](https://github.com/vexxhost/migratekit/pull/155) | fix(target): track owned volume attachments | Pending Review | Cherry-pick Portions | `upstream/pr-155` | P3 | Conflicts with this fork's multi-disk attach/detach fix; reconcile manually. |
| [#152](https://github.com/vexxhost/migratekit/pull/152) | chore(deps): update fedora docker tag to v45 | Deferred | Deferred | `upstream/pr-152` | P4 | Conflicts directionally with Fedora rollback in #138; needs runtime validation. |
| [#140](https://github.com/vexxhost/migratekit/pull/140) | [StepSecurity] Apply security best practices | Deferred | Deferred | `upstream/pr-140` | P4 | Patch conflicts with local workflow; requires CI security policy decision. |
| [#138](https://github.com/vexxhost/migratekit/pull/138) | Fix Windows Server 2022 infinite reboot loop by reverting to Fedora 42 | Deferred | Deferred | `upstream/pr-138` | P4 | Draft PR; requires Windows Server validation before base-image rollback. |
| [#122](https://github.com/vexxhost/migratekit/pull/122) | chore(deps): update dependency go to v1.26.4 | Deferred | Deferred | `upstream/pr-122` | P4 | Defer until this fork chooses Go 1.26. |
| [#115](https://github.com/vexxhost/migratekit/pull/115) | Add local disk target for incremental backup | Deferred | Deferred | `upstream/pr-115` | P5 | Large feature; patch conflicts with `main.go`; needs design review. |

## Integration History

| Date | PR Number | Summary | Validation performed | Merge commit | Related investigation |
| --- | --- | --- | --- | --- | --- |
| 2026-06-28 | [#154](https://github.com/vexxhost/migratekit/pull/154) | Scoped VMware VDDK `LD_LIBRARY_PATH` to the `nbdkit` child process instead of mutating the migratekit process environment. | `go fmt ./...`; `go vet ./...`; `go test ./...` | Pending fork PR/merge | N/A |
