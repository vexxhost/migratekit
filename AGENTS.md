# AGENTS.md

## Project purpose

This repository is a refined fork of VEXXHOST `migratekit`, a near-live VMware-to-OpenStack migration toolkit.

The objective of this fork is to extend and refine the upstream implementation while remaining compatible with upstream whenever practical.

---

## Development Environment (Required)

**All development is performed locally inside the project's Docker development container.**

Do **not** assume dependencies are installed on the host operating system.

Before performing any coding tasks, start the development environment:

```bash
docker run -it --rm --privileged \
  --network host \
  -v /dev:/dev \
  -v /usr/lib64/vmware-vix-disklib/:/usr/lib64/vmware-vix-disklib:ro \
  -v $(pwd):/app \
  --entrypoint /bin/bash \
  fedora:40
```

Inside the container install the required development packages:

```bash
dnf install -y \
    nbdkit \
    nbdkit-vddk-plugin \
    libnbd \
    libnbd-devel \
    golang \
    virt-v2v
```

Then change into the project directory:

```bash
cd /app
```

All builds, testing, formatting, debugging, and development work should occur from within this container.

---

# Development Workflow

## Branching Strategy

Follow Git best practices.

* **Never develop directly on `main`.**
* **Never commit directly to `main`.**
* **Never push directly to `main`.**
* Always start new work from the latest `main`:

```bash
git checkout main
git pull --ff-only origin main
git checkout -b <branch-name>
```

Use descriptive branch names:

* `bugfix/<name>`
* `feature/<name>`
* `docs/<name>`
* `upstream/pr-<number>`
* `experimental/<name>`

Every change must be reviewed through a Pull Request before merging into `main`.

Always sign commits:

```bash
git commit -s -m "Commit message"
```

---

## Development Lifecycle

Every task should follow this workflow:

1. Investigate
2. Document
3. Implement
4. Validate
5. Commit
6. Push
7. Open Pull Request
8. Review
9. Merge
10. Update documentation

Do not skip the investigation step for non-trivial bugs.

---

## Standard Validation

Before considering any task complete, execute:

```bash
go fmt ./...
go vet ./...
go test ./...
```

If module dependencies changed:

```bash
go mod tidy
```

If integration tests cannot be executed, explain exactly why.

Never claim testing was performed if it was not.

---

## Documentation Requirements

Significant work should update the appropriate documentation.

Possible documents include:

* `docs/architecture.md`
* `docs/investigations/`
* `docs/fork-history.md`
* `docs/upstream-review.md`
* `docs/upstream-sync.md`

Every completed investigation should include:

* Root cause
* Resolution
* Validation
* Lessons learned
* Future regression tests

Every merged feature or bug fix should add an entry to `docs/fork-history.md`.

---

## Upstream Synchronization

Before implementing new functionality or fixing a bug:

* Review upstream pull requests.
* Determine whether the issue has already been addressed upstream.
* Prefer integrating upstream-compatible fixes rather than reimplementing them.
* Avoid duplicating work already under active upstream development.

When evaluating upstream changes:

* Document the review in `docs/upstream-review.md`.
* Maintain long-term tracking in `docs/upstream-sync.md`.

When integrating upstream work:

* Create a dedicated branch:

```
upstream/pr-<number>
```

* Review the implementation.
* Validate locally.
* Open a Pull Request into this fork.
* Update `docs/upstream-sync.md`.
* Update `docs/fork-history.md` if the merge materially changes the fork.

Never merge upstream code directly into `main`.

---

## Environment Assumptions

The local workstation is expected to provide:

* Docker
* Fedora 40 development container
* Privileged container support
* Host networking
* `/dev` mounted into the container
* VMware VDDK installed under:

```
/usr/lib64/vmware-vix-disklib/
```

This path is mounted read-only into the development container.

If VMware VDDK is unavailable, stop and explain that VDDK-backed development or integration testing cannot proceed until it is installed.

Do not attempt to replace VMware VDDK with alternative libraries.

---

## Validation Expectations

Before submitting changes:

* Ensure the project builds successfully.
* Run all unit tests.
* Report any skipped integration tests.
* Explain why any tests could not be executed.
* Never claim code has been tested if it has not.

---

## Migration Safety

Migration correctness always takes precedence over optimization.

Avoid introducing changes that could:

* Corrupt migrated disks.
* Lose incremental synchronization state.
* Modify source VMware infrastructure outside of cutover.
* Expose credentials in logs.
* Make retries unsafe.

When modifying migration logic, favor correctness, traceability, recoverability, and idempotency over performance.

---

## Upstream Compatibility

Classify every completed change as one of:

* **Upstream-compatible**
* **Local-customization**
* **Experimental**

Prefer upstream-compatible implementations whenever feasible.

---

## Expected Response Format

Before making changes:

* Summarize the planned work in 2–4 bullets.

After completing work, report:

* Files modified
* Commands executed
* Tests passed
* Tests skipped
* Manual validation performed
* Remaining risks
* Compatibility classification
* Whether documentation was updated
