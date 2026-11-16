package vmware_nbdkit

import (
	"context"
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
	"github.com/vexxhost/migratekit/internal/target"
	"github.com/vexxhost/migratekit/internal/vmware"
)

func (s *NbdkitServer) convertTarget(ctx context.Context, t target.Target, path string) error {
	log.Info("Running virt-v2v-in-place")
	args := []string{"-i", "disk", path}
	if s.Servers.VddkConfig.Debug {
		args = append([]string{"-v", "-x"}, args...)
	}
	cmd := exec.Command("virt-v2v-in-place", args...)
	return runConversion(ctx, []target.Target{t}, cmd)
}

func runConversion(ctx context.Context, targets []target.Target, cmd *exec.Cmd) error {
	// Conversion can modify any input volume even when it fails. Invalidate
	// every checkpoint before allowing the converter to start.
	for _, t := range targets {
		if err := t.WriteChangeID(ctx, &vmware.ChangeID{}); err != nil {
			return err
		}
	}
	cmd.Env = append(os.Environ(), "LIBGUESTFS_BACKEND=direct")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
