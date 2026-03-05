FROM fedora:44@sha256:b3242de4d1022bf74f207f9b8462daa183c24b85c5997a97cd1d7dd6e6359a9b AS build
RUN dnf install -y golang libnbd-devel
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /migratekit main.go

FROM fedora:44@sha256:b3242de4d1022bf74f207f9b8462daa183c24b85c5997a97cd1d7dd6e6359a9b
ADD https://fedorapeople.org/groups/virt/virtio-win/virtio-win.repo /etc/yum.repos.d/virtio-win.repo
RUN \
  dnf install --refresh -y nbdkit nbdkit-vddk-plugin libnbd virt-v2v virtio-win && \
  dnf clean all && \
  rm -rf /var/cache/dnf
COPY --from=build /migratekit /usr/local/bin/migratekit
ENTRYPOINT ["/usr/local/bin/migratekit"]
