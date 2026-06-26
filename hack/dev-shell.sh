#!/usr/bin/env bash
set -euo pipefail

docker run -it --rm --privileged \
  --network host \
  -v /dev:/dev \
  -v /usr/lib64/vmware-vix-disklib/:/usr/lib64/vmware-vix-disklib:ro \
  -v "$(pwd):/app" \
  --entrypoint /bin/bash \
  fedora:40