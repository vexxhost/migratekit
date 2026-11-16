package vmware_nbdkit

import (
	"context"
	"os"
	"os/exec"

	"github.com/vexxhost/migratekit/internal/target"
	"github.com/vexxhost/migratekit/internal/vmware"
)

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
