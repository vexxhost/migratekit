#!/bin/bash
set -euo pipefail

if (( $# > 1 )); then
  printf 'Usage: %s [image-tag]\n' "$0" >&2
  exit 2
fi

repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
image_tag="${1:-migratekit:windows-server-2012-r2}"
driver_rpm='https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/archive-virtio/virtio-win-0.1.190-1/virtio-win-0.1.190-1.noarch.rpm'

docker build \
  --build-arg "VIRTIO_WIN_PACKAGE=$driver_rpm" \
  --tag "$image_tag" \
  "$repo_dir"
