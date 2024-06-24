FROM fedora:40 AS build
RUN dnf install -y golang libnbd-devel
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /migratekit main.go

FROM fedora:40
RUN dnf install -y nbdkit nbdkit-vddk-plugin libnbd virt-v2v
COPY --from=build /migratekit /usr/local/bin/migratekit
ENTRYPOINT ["/usr/local/bin/migratekit"]
