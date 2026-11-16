FROM fedora:44 AS build
RUN dnf install -y golang libnbd-devel
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /migratekit main.go

FROM fedora:44
ARG VIRTIO_WIN_PACKAGE=virtio-win
ADD https://fedorapeople.org/groups/virt/virtio-win/virtio-win.repo /etc/yum.repos.d/virtio-win.repo
# The SMBus catalog is signed with a development certificate that Windows does
# not trust. Its deterministic installation failure makes virt-v2v 2.12 retry
# firstboot indefinitely. Keep the remaining drivers and certificate checks.
RUN \
  dnf install --refresh -y nbdkit nbdkit-vddk-plugin libnbd virt-v2v "$VIRTIO_WIN_PACKAGE" && \
  find /usr/share/virtio-win -iname 'smbus.*' -delete && \
  dnf clean all && \
  rm -rf /var/cache/dnf
# Prefer the curated unpacked tree over the package's unmodified driver ISO.
ENV VIRTIO_WIN=/usr/share/virtio-win
COPY --from=build /migratekit /usr/local/bin/migratekit
ENTRYPOINT ["/usr/local/bin/migratekit"]
