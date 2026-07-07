# Upstream Pull Request Review

Review date: 2026-06-28

Upstream repository: <https://github.com/vexxhost/migratekit/pulls>

This is a point-in-time review of every open upstream pull request. It is based
on GitHub API metadata, PR file lists, PR patch contents, and a non-mutating
`git apply --check` compatibility check against this fork.

No upstream code was merged, cherry-picked, or applied during this review.

## 2026-07-05 Issue-Specific Review - OpenStack Token Expiry

Reviewed current open upstream PR metadata for an existing fix to stale
OpenStack tokens after long disk-copy operations.

Current open upstream PRs reviewed by title and issue relevance:

- [#164](https://github.com/vexxhost/migratekit/pull/164) - v2v with all guest
  disks; no token/session handling.
- [#163](https://github.com/vexxhost/migratekit/pull/163) - OpenStack volume
  attachment waits; touches OpenStack target code but the patch contains no
  `auth`, `token`, `401`, `AllowReauth`, or `Reauth` changes.
- [#124](https://github.com/vexxhost/migratekit/pull/124) - Gophercloud
  dependency update only (`go.mod`, `go.sum`).
- Remaining open PRs are dependency, CI, Fedora image, VMware, CLI, or local
  target changes and do not address OpenStack authentication refresh.

Conclusion: no open upstream PR addresses the stale OpenStack token failure.
The local fix in
`docs/investigations/openstack-token-expiry-long-copy.md` remains
upstream-compatible because it enables Gophercloud's built-in bounded
reauthentication behavior.

## Executive Summary

Total PRs reviewed: 17

Most upstream PRs are dependency, CI, or container-base updates. The safest
near-term integrations are small, isolated changes:

1. [#154](https://github.com/vexxhost/migratekit/pull/154) - scope
   `LD_LIBRARY_PATH` to the `nbdkit` child process.
2. [#141](https://github.com/vexxhost/migratekit/pull/141) - add a top-level
   least-privilege GitHub Actions permissions block.
3. Low-risk dependency updates that compile cleanly one at a time.

The highest-risk PR is [#155](https://github.com/vexxhost/migratekit/pull/155)
because it changes the same OpenStack attach/detach code this fork has already
modified for the multi-disk detach timeout fix. It does not apply cleanly to
this fork and should be integrated manually, if at all.

The largest feature PR is [#115](https://github.com/vexxhost/migratekit/pull/115),
which adds a local disk target. It is broad, conflicts with this fork's `main.go`,
and appears incomplete for production migration semantics.

## Priority Merge Order

1. [#154](https://github.com/vexxhost/migratekit/pull/154) -
   Merge Immediately.
2. [#141](https://github.com/vexxhost/migratekit/pull/141) -
   Merge Immediately after confirming CI policy.
3. [#132](https://github.com/vexxhost/migratekit/pull/132),
   [#143](https://github.com/vexxhost/migratekit/pull/143), and
   [#130](https://github.com/vexxhost/migratekit/pull/130) -
   Merge After Review, one at a time.
4. [#146](https://github.com/vexxhost/migratekit/pull/146),
   [#131](https://github.com/vexxhost/migratekit/pull/131),
   [#145](https://github.com/vexxhost/migratekit/pull/145),
   [#147](https://github.com/vexxhost/migratekit/pull/147), and
   [#124](https://github.com/vexxhost/migratekit/pull/124) -
   Merge After Review with focused runtime validation.
5. [#134](https://github.com/vexxhost/migratekit/pull/134) -
   Merge After Review only after cleanup and CLI validation.
6. [#155](https://github.com/vexxhost/migratekit/pull/155) -
   Cherry-pick Portions / manual reconciliation with this fork's attach/detach
   fix.
7. [#152](https://github.com/vexxhost/migratekit/pull/152),
   [#138](https://github.com/vexxhost/migratekit/pull/138), and
   [#140](https://github.com/vexxhost/migratekit/pull/140) -
   Deferred until container and CI policy decisions are made.
8. [#122](https://github.com/vexxhost/migratekit/pull/122) -
   Deferred until Go 1.26 is desired for this fork.
9. [#115](https://github.com/vexxhost/migratekit/pull/115) -
   Deferred as a separate feature design effort.

## High-Risk Integrations

- [#155](https://github.com/vexxhost/migratekit/pull/155):
  overlaps the fork's OpenStack attach/detach timeout work and fails
  `git apply --check`.
- [#115](https://github.com/vexxhost/migratekit/pull/115):
  broad feature addition touching CLI flow, target selection, and migration
  semantics; fails `git apply --check`.
- [#124](https://github.com/vexxhost/migratekit/pull/124):
  Gophercloud upgrade affects OpenStack volume, attachment, server, and network
  calls.
- [#147](https://github.com/vexxhost/migratekit/pull/147):
  libnbd upgrade affects the incremental-copy data path.
- [#138](https://github.com/vexxhost/migratekit/pull/138) and
  [#152](https://github.com/vexxhost/migratekit/pull/152):
  conflicting Fedora base-image direction with direct impact on `virt-v2v`,
  `virtio-win`, and VDDK runtime compatibility.

## Potential Merge Conflicts

Patch compatibility against this fork:

- Does not apply cleanly:
  [#155](https://github.com/vexxhost/migratekit/pull/155),
  [#140](https://github.com/vexxhost/migratekit/pull/140),
  [#115](https://github.com/vexxhost/migratekit/pull/115).
- Applies cleanly in isolation:
  [#154](https://github.com/vexxhost/migratekit/pull/154),
  [#152](https://github.com/vexxhost/migratekit/pull/152),
  [#147](https://github.com/vexxhost/migratekit/pull/147),
  [#146](https://github.com/vexxhost/migratekit/pull/146),
  [#145](https://github.com/vexxhost/migratekit/pull/145),
  [#143](https://github.com/vexxhost/migratekit/pull/143),
  [#141](https://github.com/vexxhost/migratekit/pull/141),
  [#138](https://github.com/vexxhost/migratekit/pull/138),
  [#134](https://github.com/vexxhost/migratekit/pull/134),
  [#132](https://github.com/vexxhost/migratekit/pull/132),
  [#131](https://github.com/vexxhost/migratekit/pull/131),
  [#130](https://github.com/vexxhost/migratekit/pull/130),
  [#124](https://github.com/vexxhost/migratekit/pull/124),
  [#122](https://github.com/vexxhost/migratekit/pull/122).

Logical conflict groups:

- `internal/target/openstack.go` and `internal/target/openstack_test.go`:
  [#155](https://github.com/vexxhost/migratekit/pull/155) overlaps the
  multi-disk detach timeout fix in this fork.
- `.github/workflows/ci.yaml`:
  [#140](https://github.com/vexxhost/migratekit/pull/140) conflicts with the
  newer local workflow action versions; [#141](https://github.com/vexxhost/migratekit/pull/141)
  is smaller and applies cleanly.
- `Dockerfile`:
  [#152](https://github.com/vexxhost/migratekit/pull/152),
  [#140](https://github.com/vexxhost/migratekit/pull/140), and
  [#138](https://github.com/vexxhost/migratekit/pull/138) all change the
  container base image or pinning strategy.
- `go.mod` and `go.sum`:
  dependency PRs apply individually but should be integrated one at a time to
  avoid unnecessary module churn.
- `main.go`:
  [#134](https://github.com/vexxhost/migratekit/pull/134) and
  [#115](https://github.com/vexxhost/migratekit/pull/115) both affect CLI or
  command context behavior.

## Recommended Validation After Each Integration

Always run the standard workflow from `AGENTS.md` in the Fedora development
container:

```bash
go fmt ./...
go vet ./...
go test ./...
```

Additional validation by PR type:

- OpenStack attach/detach changes:
  run multi-disk `migrate` and `cutover` against live OpenStack, including a
  fast incremental copy with little or no changed data.
- VMware/govmomi changes:
  verify VMware login, VM lookup, CBT validation, snapshot creation/removal, and
  `QueryChangedDiskAreas`.
- libnbd changes:
  validate incremental copy correctness with multiple changed extents.
- Gophercloud changes:
  validate volume lookup, volume attach/detach, Cinder metadata updates, Neutron
  port creation, and Nova server creation.
- CLI changes:
  validate interactive and non-interactive invocation paths.
- Docker/Fedora changes:
  build the image, verify VDDK loading, run `nbdkit`, run `virt-v2v-in-place`,
  and test Windows Server guests if `virtio-win` changes.
- CI changes:
  open a test pull request and confirm image build, permissions, attestations,
  and GHCR push behavior.

## Detailed PR Reviews

### [#155](https://github.com/vexxhost/migratekit/pull/155) - fix(target): track owned volume attachments

- Author: `ricolin`
- Purpose: Track when migratekit attaches an OpenStack volume itself, and detach
  only volumes attached by the current run.
- Files modified:
  - `internal/target/openstack.go`
  - `internal/target/openstack_test.go`
- Appears complete: Partially. It has unit coverage and a coherent ownership
  model, but it does not include this fork's attach-readiness wait.
- Conflicts with this fork: Yes. `git apply --check` fails on
  `internal/target/openstack.go`; `internal/target/openstack_test.go` already
  exists locally.
- Overlaps completed fork work: Yes. This fork already changed
  `OpenStack.Connect`, `OpenStack.Disconnect`, and helper tests for the
  multi-disk detach timeout issue.
- Risk level: High.
- Estimated integration effort: Medium to high.
- Classification: Cherry-pick Portions.
- Recommendation: Do not merge as-is. Manually review the ownership-tracking
  idea and reconcile it with this fork's `waitForVolumeAttached`,
  `waitForVolumeDetached`, and multi-disk tests. Be careful not to regress
  detach confirmation or attach readiness.

### [#154](https://github.com/vexxhost/migratekit/pull/154) - fix(nbdkit): scope VDDK library path to nbdkit

- Author: `rhochmayr`
- Purpose: Set `LD_LIBRARY_PATH` only on the `nbdkit` child process instead of
  mutating the migratekit process environment globally.
- Files modified:
  - `internal/nbdkit/builder.go`
- Appears complete: Yes.
- Conflicts with this fork: No. Patch applies cleanly.
- Overlaps completed fork work: No.
- Risk level: Low.
- Estimated integration effort: Low.
- Classification: Merge Immediately.
- Recommendation: Integrate first. It is small, upstream-compatible, and reduces
  environment leakage into later child processes such as `virt-v2v-in-place`.

### [#152](https://github.com/vexxhost/migratekit/pull/152) - chore(deps): update fedora docker tag to v45

- Author: `renovate[bot]`
- Purpose: Update both Dockerfile stages from `fedora:44` to `fedora:45`.
- Files modified:
  - `Dockerfile`
- Appears complete: Yes as a Renovate base-image bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: High.
- Estimated integration effort: Medium.
- Classification: Deferred.
- Recommendation: Defer until Windows, VDDK, `nbdkit`, and `virt-v2v`
  validation are available. This conflicts directionally with #138, which
  proposes downgrading Fedora for Windows Server 2022 compatibility.

### [#147](https://github.com/vexxhost/migratekit/pull/147) - fix(deps): update module libguestfs.org/libnbd to v1.25.4

- Author: `renovate[bot]`
- Purpose: Update Go libnbd bindings from `v1.22.2-4-g3d7cc461d` to `v1.25.4`.
- Files modified:
  - `go.mod`
  - `go.sum`
- Appears complete: Yes as a dependency bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: Medium.
- Estimated integration effort: Low to medium.
- Classification: Merge After Review.
- Recommendation: Integrate after smaller dependency bumps. Validate
  incremental-copy behavior against real changed-block data because libnbd is on
  the data path.

### [#146](https://github.com/vexxhost/migratekit/pull/146) - fix(deps): update module github.com/vmware/govmomi to v0.55.0

- Author: `renovate[bot]`
- Purpose: Update govmomi from `v0.52.0` to `v0.55.0`.
- Files modified:
  - `go.mod`
  - `go.sum`
- Appears complete: Yes as a dependency bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: Medium.
- Estimated integration effort: Low to medium.
- Classification: Merge After Review.
- Recommendation: Integrate with VMware-focused validation: login, VM lookup,
  snapshot handling, CBT checks, and `QueryChangedDiskAreas`.

### [#145](https://github.com/vexxhost/migratekit/pull/145) - fix(deps): update module github.com/erikgeiser/promptkit to v0.11.0

- Author: `renovate[bot]`
- Purpose: Update promptkit from `v0.9.0` to `v0.11.0`.
- Files modified:
  - `go.mod`
  - `go.sum`
- Appears complete: Yes as a dependency bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: Low to medium.
- Estimated integration effort: Low.
- Classification: Merge After Review.
- Recommendation: Integrate after low-risk dependencies. Validate the existing
  interactive snapshot-removal prompt.

### [#143](https://github.com/vexxhost/migratekit/pull/143) - fix(deps): update module github.com/spf13/cobra to v1.10.2

- Author: `renovate[bot]`
- Purpose: Update Cobra from `v1.10.1` to `v1.10.2`.
- Files modified:
  - `go.mod`
  - `go.sum`
- Appears complete: Yes as a dependency bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: Low.
- Estimated integration effort: Low.
- Classification: Merge After Review.
- Recommendation: Integrate early with CLI smoke tests for `migratekit`,
  `migratekit migrate --help`, and `migratekit cutover --help`.

### [#141](https://github.com/vexxhost/migratekit/pull/141) - ci: enforce least-privilege permissions for GitHub Actions workflows

- Author: `larainema`
- Purpose: Add a top-level `permissions: {}` block to the GitHub Actions
  workflow.
- Files modified:
  - `.github/workflows/ci.yaml`
- Appears complete: Yes.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: Partial. The local workflow already has
  job-level permissions for image publishing.
- Risk level: Low.
- Estimated integration effort: Low.
- Classification: Merge Immediately.
- Recommendation: Integrate after #154 or in its own PR. Confirm that the
  existing job-level permissions still permit GHCR push and attestations.

### [#140](https://github.com/vexxhost/migratekit/pull/140) - [StepSecurity] Apply security best practices

- Author: `stepsecurity-app[bot]`
- Purpose: Add StepSecurity hardening, use StepSecurity wrapper actions, set
  workflow permissions, and pin Fedora Docker images by digest.
- Files modified:
  - `.github/workflows/ci.yaml`
  - `Dockerfile`
- Appears complete: Mechanically complete, but policy-heavy.
- Conflicts with this fork: Yes. `git apply --check` fails on
  `.github/workflows/ci.yaml` because this fork already has newer action pins.
- Overlaps completed fork work: Partial. Local workflow action versions are
  newer than the upstream patch context.
- Risk level: Medium.
- Estimated integration effort: Medium.
- Classification: Deferred.
- Recommendation: Do not merge as-is. Decide separately whether this fork wants
  StepSecurity wrapper actions, egress auditing, and Docker digest pinning.
  Consider cherry-picking only the harden-runner idea after CI policy review.

### [#138](https://github.com/vexxhost/migratekit/pull/138) - Fix Windows Server 2022 infinite reboot loop by reverting to Fedora 42

- Author: `Copilot`
- Purpose: Downgrade Dockerfile base images from Fedora 44 to Fedora 42 and add
  documentation about Windows Server 2022 reboot loops after migration.
- Files modified:
  - `Dockerfile`
  - `WINDOWS_SERVER_2022_FIX.md`
- Appears complete: No. The PR is marked draft and the proposed fix is broad.
- Conflicts with this fork: No direct patch conflict, but it conflicts
  directionally with #152 and #140.
- Overlaps completed fork work: No.
- Risk level: High.
- Estimated integration effort: Medium to high.
- Classification: Deferred.
- Recommendation: Defer. Validate the Windows Server 2022 issue independently
  before downgrading the whole runtime image. A narrower package pin or
  documented workaround may be safer than a base-image rollback.

### [#134](https://github.com/vexxhost/migratekit/pull/134) - Closes Issue #133 - removes required vmware-password argument

- Author: `joelmclean`
- Purpose: Make `--vmware-password` optional and prompt interactively when it is
  not provided.
- Files modified:
  - `main.go`
- Appears complete: Partially. It needs formatting review and CLI tests; it
  imports `golang.org/x/term`, which is currently indirect in this fork.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: Medium.
- Estimated integration effort: Low to medium.
- Classification: Merge After Review.
- Recommendation: Integrate only after cleanup. Validate interactive terminal,
  non-interactive failure, Docker usage, and existing flag behavior.

### [#132](https://github.com/vexxhost/migratekit/pull/132) - fix(deps): update module github.com/sirupsen/logrus to v1.9.4

- Author: `renovate[bot]`
- Purpose: Update logrus from `v1.9.3` to `v1.9.4`.
- Files modified:
  - `go.mod`
  - `go.sum`
- Appears complete: Yes as a dependency bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: Low.
- Estimated integration effort: Low.
- Classification: Merge After Review.
- Recommendation: Integrate early. Validate log output at normal and debug
  levels.

### [#131](https://github.com/vexxhost/migratekit/pull/131) - fix(deps): update module github.com/thediveo/enumflag/v2 to v2.2.1

- Author: `renovate[bot]`
- Purpose: Update enumflag from `v2.0.7` to `v2.2.1`.
- Files modified:
  - `go.mod`
  - `go.sum`
- Appears complete: Yes as a dependency bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: Medium.
- Estimated integration effort: Low to medium.
- Classification: Merge After Review.
- Recommendation: Validate enum-backed CLI flags:
  `--compression-method` and `--disk-bus-type`.

### [#130](https://github.com/vexxhost/migratekit/pull/130) - fix(deps): update module github.com/schollz/progressbar/v3 to v3.19.0

- Author: `renovate[bot]`
- Purpose: Update progressbar from `v3.18.0` to `v3.19.0`.
- Files modified:
  - `go.mod`
  - `go.sum`
- Appears complete: Yes as a dependency bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: Low.
- Estimated integration effort: Low.
- Classification: Merge After Review.
- Recommendation: Integrate early. Validate full-copy and incremental-copy
  progress output.

### [#124](https://github.com/vexxhost/migratekit/pull/124) - fix(deps): update module github.com/gophercloud/gophercloud/v2 to v2.13.0

- Author: `renovate[bot]`
- Purpose: Update Gophercloud from `v2.8.0` to `v2.13.0`.
- Files modified:
  - `go.mod`
  - `go.sum`
- Appears complete: Yes as a dependency bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: Indirectly. This fork's multi-disk detach fix
  relies on Cinder volume and Nova attachment behavior through Gophercloud.
- Risk level: Medium to high.
- Estimated integration effort: Medium.
- Classification: Merge After Review.
- Recommendation: Integrate after the OpenStack attach/detach behavior is
  stable. Validate Cinder volume lookup, metadata, attach, detach polling,
  Neutron port creation, and Nova boot.

### [#122](https://github.com/vexxhost/migratekit/pull/122) - chore(deps): update dependency go to v1.26.4

- Author: `renovate[bot]`
- Purpose: Update `mise.toml` Go tool pin from `1.25.1` to `1.26.4`.
- Files modified:
  - `mise.toml`
- Appears complete: Yes as a tool-version bump.
- Conflicts with this fork: No direct patch conflict.
- Overlaps completed fork work: No.
- Risk level: Medium.
- Estimated integration effort: Low to medium.
- Classification: Deferred.
- Recommendation: Defer until the project intentionally moves to Go 1.26.
  Validate the Fedora development-container workflow and `GOTOOLCHAIN` behavior
  before adopting.

### [#115](https://github.com/vexxhost/migratekit/pull/115) - Add local disk target for incremental backup

- Author: `legitYosal`
- Purpose: Add a local disk target for raw backup files and metadata-backed
  incremental backup tracking.
- Files modified:
  - `README.md`
  - `internal/target/local.go`
  - `internal/vmware_nbdkit/vmware_nbdkit.go`
  - `main.go`
- Appears complete: No. The PR body says it is a starting point and was created
  with AI assistance. The patch is broad and needs design review.
- Conflicts with this fork: Yes. `git apply --check` fails on `main.go`.
- Overlaps completed fork work: No direct overlap, but it touches the migration
  cycle and target abstraction used by this fork's OpenStack fixes.
- Risk level: High.
- Estimated integration effort: High.
- Classification: Deferred.
- Recommendation: Do not integrate during upstream sync. Treat local-disk backup
  as a separate feature design with tests for file sizing, sparse behavior,
  incremental writes, metadata durability, `virt-v2v` semantics, and cutover
  behavior.

## Integration Plan

### Phase 1: Small, isolated fixes

1. Integrate #154 on branch `upstream/pr-154`.
   - Why now: isolated one-line runtime hygiene fix.
   - Dependencies: none.
   - Regression tests: standard Go workflow, plus a manual `nbdkit` start in
     the development container if VDDK is available.
2. Integrate #141 on branch `upstream/pr-141`.
   - Why now: low-risk CI hardening that applies cleanly.
   - Dependencies: none.
   - Regression tests: open a PR and verify CI image build permissions.

### Phase 2: Low-risk dependency updates

Integrate #132, #143, and #130 one at a time.

- Why now: small dependency updates with low behavioral surface.
- Dependencies: none, but module updates should be serialized.
- Regression tests: standard Go workflow, CLI help smoke tests, and progress/log
  output sanity checks.

### Phase 3: Runtime-sensitive dependency updates

Integrate #146, #131, #145, #147, and #124 one at a time.

- Why now: these keep the fork closer to upstream but touch VMware, CLI parsing,
  prompts, libnbd, or OpenStack APIs.
- Dependencies: none required, but later dependency updates may alter `go.sum`
  context.
- Regression tests: standard Go workflow plus targeted VMware, libnbd, and
  OpenStack validation as listed above.

### Phase 4: CLI ergonomics

Review and clean up #134 on branch `upstream/pr-134`.

- Why now: optional password prompting may be useful but changes CLI behavior.
- Dependencies: decide whether `golang.org/x/term` should become a direct
  dependency.
- Regression tests: interactive prompt, non-interactive Docker execution,
  explicit `--vmware-password`, and missing-password error messages.

### Phase 5: Conflicting OpenStack behavior

Manually reconcile #155 on branch `upstream/pr-155`.

- Why now: ownership tracking may be valuable, but it directly overlaps this
  fork's multi-disk detach timeout fix.
- Dependencies: preserve the current attach readiness and detach completion
  behavior.
- Regression tests: multi-disk `migrate` and `cutover`, pre-attached target
  volume behavior, fast incremental copies, and detach wait error handling.

### Phase 6: Container, CI policy, and feature work

Defer #152, #140, #138, #122, and #115 until separate decisions are made.

- Why not now: these require runtime policy or feature design decisions beyond
  simple upstream synchronization.
- Dependencies: Windows Server validation for Fedora base changes; CI security
  policy for StepSecurity; Go toolchain policy for Go 1.26; design review for
  local disk targets.
- Regression tests: full image build, VDDK load, `virt-v2v`, Windows guest
  boot, CI publication, and feature-specific functional tests.
