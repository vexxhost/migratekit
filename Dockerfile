FROM fedora:44 AS build
RUN dnf install -y golang libnbd-devel
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /migratekit main.go

FROM fedora:44 AS runtime
ADD https://fedorapeople.org/groups/virt/virtio-win/virtio-win.repo /etc/yum.repos.d/virtio-win.repo
RUN \
  dnf install --refresh -y nbdkit nbdkit-vddk-plugin libnbd virt-v2v virtio-win && \
  dnf clean all && \
  rm -rf /var/cache/dnf
COPY --from=build /migratekit /usr/local/bin/migratekit
ENTRYPOINT ["/usr/local/bin/migratekit"]

FROM fedora:44 AS openvddk-download
# The upstream release is Linux x86-64 only. Verify both the release archive
# and its file manifest, and retain the license, notices and SBOM with it.
ADD --checksum=sha256:4f50188fcfe9dc8c0e523a05359bee16c12f990f812332548f5b00a4c2252687 \
    https://github.com/openvddk/openvddk/releases/download/v0.7.4/openvddk-evalkit-0.7.4-94926da2f764.tar.gz /tmp/openvddk.tar.gz
RUN test "$(uname -m)" = x86_64 && \
    mkdir -p /opt/openvddk && \
    tar -xzf /tmp/openvddk.tar.gz --strip-components=1 -C /opt/openvddk && \
    cd /opt/openvddk && sha256sum -c SHA256SUMS && \
    ln -s lib lib64

FROM runtime AS openvddk
COPY --from=openvddk-download /opt/openvddk/ /usr/lib64/vmware-vix-disklib/
RUN nbdkit vddk --dump-plugin > /tmp/openvddk-plugin && \
    grep -Fx 'vddk_library_version=8' /tmp/openvddk-plugin && \
    rm /tmp/openvddk-plugin
