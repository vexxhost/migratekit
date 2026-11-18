package vmware_nbdkit

import (
	"context"
	"os/exec"

	"github.com/vexxhost/migratekit/internal/process"
	"github.com/vexxhost/migratekit/internal/target"
)

func conversionCommand(ctx context.Context, debug bool, inputArgs ...string) *exec.Cmd {
	driver := "virtio-blk"
	if opts, ok := ctx.Value("volumeCreateOpts").(*target.VolumeCreateOpts); ok && opts.BusType == "scsi" {
		driver = "virtio-scsi"
	}
	args := []string{"--block-driver", driver}
	if keys, ok := ctx.Value("luksKeys").([]string); ok {
		for _, key := range keys {
			args = append(args, "--key", key)
		}
	}
	if mappings, ok := ctx.Value("guestNetworkConfigs").([]string); ok {
		for _, mapping := range mappings {
			args = append(args, "--mac", mapping)
		}
	}
	if debug {
		args = append([]string{"-v", "-x"}, args...)
	}
	args = append(args, inputArgs...)
	return process.CommandContext(ctx, "virt-v2v-in-place", args...)
}
