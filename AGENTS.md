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

All builds, testing, formatting, and development work should occur from within this container.

---

## Development Workflow

Before considering any task complete, execute:

```bash
go fmt ./...
go vet ./...
go test ./...
```

If module dependencies have changed, also execute:

```bash
go mod tidy
```

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

* corrupt migrated disks
* lose incremental synchronization state
* modify source VMware infrastructure outside of cutover
* expose credentials in logs
* make retries unsafe

When modifying migration logic, favor correctness, traceability, and recoverability over performance.

---

## Upstream Compatibility

Classify every change as one of:

* **upstream-compatible**
* **local-customization**
* **experimental**

Prefer upstream-compatible implementations whenever feasible.

---

## Expected Response Format

Before making changes:

* Summarize the planned work in 2–4 bullets.

After completing work, report:

* Files modified
* Commands executed
* Test results
* Any skipped validation
* Remaining risks
* Compatibility classification
