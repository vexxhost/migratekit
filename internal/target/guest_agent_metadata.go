package target

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
)

// The agent can be requested after replication has already created the disk.
// Nova reads this property when creating the destination server, so update it
// before attachment and cutover rather than only during volume creation.
func (t *OpenStack) ensureGuestAgentMetadata(ctx context.Context, volumeID string) error {
	enabled, _ := ctx.Value("enableQemuGuestAgent").(bool)
	if !enabled {
		return nil
	}
	volume, err := volumes.Get(ctx, t.ClientSet.BlockStorage, volumeID).Extract()
	if err != nil {
		return fmt.Errorf("reading volume guest agent image metadata: %w", err)
	}
	if volume.VolumeImageMetadata["hw_qemu_guest_agent"] == "yes" {
		return nil
	}
	// Cinder merges these image properties; do not replace unrelated settings.
	err = volumes.SetImageMetadata(ctx, t.ClientSet.BlockStorage, volumeID, volumes.ImageMetadataOpts{
		Metadata: map[string]string{"hw_qemu_guest_agent": "yes"},
	}).ExtractErr()
	if err != nil {
		return fmt.Errorf("enabling volume guest agent image metadata: %w", err)
	}
	return nil
}
