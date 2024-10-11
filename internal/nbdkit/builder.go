package nbdkit

import (
	"fmt"
	"os"
	"os/exec"
)

type CompressionMethod string

const (
	NoCompression     CompressionMethod = "none"
	ZlibCompression   CompressionMethod = "zlib"
	FastLzCompression CompressionMethod = "fastlz"
	SkipzCompression  CompressionMethod = "skipz"
)

type NbdkitBuilder struct {
	server      string
	username    string
	password    string
	thumbprint  string
	vm          string
	snapshot    string
	filename    string
	compression CompressionMethod
}

func NewNbdkitBuilder() *NbdkitBuilder {
	return &NbdkitBuilder{}
}

func (b *NbdkitBuilder) Server(server string) *NbdkitBuilder {
	b.server = server
	return b
}

func (b *NbdkitBuilder) Username(username string) *NbdkitBuilder {
	b.username = username
	return b
}

func (b *NbdkitBuilder) Password(password string) *NbdkitBuilder {
	b.password = password
	return b
}

func (b *NbdkitBuilder) Thumbprint(thumbprint string) *NbdkitBuilder {
	b.thumbprint = thumbprint
	return b
}

func (b *NbdkitBuilder) VirtualMachine(vm string) *NbdkitBuilder {
	b.vm = vm
	return b
}

func (b *NbdkitBuilder) Snapshot(snapshot string) *NbdkitBuilder {
	b.snapshot = snapshot
	return b
}

func (b *NbdkitBuilder) Filename(filename string) *NbdkitBuilder {
	b.filename = filename
	return b
}

func (b *NbdkitBuilder) Compression(method CompressionMethod) *NbdkitBuilder {
	b.compression = method
	return b
}

func (b *NbdkitBuilder) Build() (*NbdkitServer, error) {
	tmp, err := os.MkdirTemp("", "migratekit-")
	if err != nil {
		return nil, err
	}

	socket := fmt.Sprintf("%s/nbdkit.sock", tmp)
	pidFile := fmt.Sprintf("%s/nbdkit.pid", tmp)

	os.Setenv("LD_LIBRARY_PATH", "/usr/lib64/vmware-vix-disklib/lib64")
	cmd := exec.Command(
		"nbdkit",
		"--exit-with-parent",
		"--readonly",
		"--foreground",
		fmt.Sprintf("--unix=%s", socket),
		fmt.Sprintf("--pidfile=%s", pidFile),
		"vddk",
		fmt.Sprintf("server=%s", b.server),
		fmt.Sprintf("user=%s", b.username),
		fmt.Sprintf("password=%s", b.password),
		fmt.Sprintf("thumbprint=%s", b.thumbprint),
		fmt.Sprintf("compression=%s", b.compression),
		fmt.Sprintf("vm=moref=%s", b.vm),
		fmt.Sprintf("snapshot=%s", b.snapshot),
		"transports=file:nbdssl:nbd",
		b.filename,
	)

	return &NbdkitServer{
		cmd:     cmd,
		socket:  socket,
		pidFile: pidFile,
	}, nil
}
