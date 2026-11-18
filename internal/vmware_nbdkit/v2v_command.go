package vmware_nbdkit

import (
	"context"
	"os/exec"

	"github.com/vexxhost/migratekit/internal/target"
)

func conversionCommand(ctx context.Context, debug bool, inputArgs ...string) *exec.Cmd {
	driver := "virtio-blk"
	if opts, ok := ctx.Value("volumeCreateOpts").(*target.VolumeCreateOpts); ok && opts.BusType == "scsi" {
		driver = "virtio-scsi"
	}
	args := []string{"--block-driver", driver}
	if debug {
		args = append([]string{"-v", "-x"}, args...)
	}
	args = append(args, inputArgs...)
	return exec.Command("virt-v2v-in-place", args...)
}
