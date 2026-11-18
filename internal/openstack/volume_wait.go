package openstack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	log "github.com/sirupsen/logrus"
)

const (
	DefaultVolumeCreateTimeout = 10 * time.Minute
	DefaultVolumeAttachTimeout = 10 * time.Minute
	DefaultVolumeDetachTimeout = 10 * time.Minute
)

type volumeWaitOptsContextKey struct{}

type VolumeWaitOpts struct {
	CreateTimeout time.Duration
	AttachTimeout time.Duration
	DetachTimeout time.Duration
}

func DefaultVolumeWaitOpts() VolumeWaitOpts {
	return VolumeWaitOpts{
		CreateTimeout: DefaultVolumeCreateTimeout,
		AttachTimeout: DefaultVolumeAttachTimeout,
		DetachTimeout: DefaultVolumeDetachTimeout,
	}
}

func WithVolumeWaitOpts(ctx context.Context, opts VolumeWaitOpts) context.Context {
	return context.WithValue(ctx, volumeWaitOptsContextKey{}, opts)
}

func VolumeWaitOptsFromContext(ctx context.Context) VolumeWaitOpts {
	opts, ok := ctx.Value(volumeWaitOptsContextKey{}).(VolumeWaitOpts)
	if !ok {
		return DefaultVolumeWaitOpts()
	}

	defaults := DefaultVolumeWaitOpts()
	if opts.CreateTimeout == 0 {
		opts.CreateTimeout = defaults.CreateTimeout
	}
	if opts.AttachTimeout == 0 {
		opts.AttachTimeout = defaults.AttachTimeout
	}
	if opts.DetachTimeout == 0 {
		opts.DetachTimeout = defaults.DetachTimeout
	}

	return opts
}

func volumeAttachedTo(volume *volumes.Volume, serverID string) bool {
	for _, attachment := range volume.Attachments {
		if strings.EqualFold(attachment.ServerID, serverID) {
			return true
		}
	}

	return false
}

func (c *ClientSet) WaitForVolumeAvailable(ctx context.Context, volumeID string, timeout time.Duration) (*volumes.Volume, error) {
	return c.waitForVolume(ctx, volumeID, timeout, "available with no attachments", func(volume *volumes.Volume) bool {
		return volume.Status == "available" && len(volume.Attachments) == 0
	})
}

func (c *ClientSet) EnsureVolumeAvailable(ctx context.Context, volumeID string, timeout time.Duration) (*volumes.Volume, error) {
	volume, err := volumes.Get(ctx, c.BlockStorage, volumeID).Extract()
	if err != nil {
		return nil, err
	}

	if (volume.Status == "reserved" || volume.Status == "attaching") && len(volume.Attachments) == 0 {
		log.WithFields(log.Fields{
			"volume_id": volume.ID,
			"status":    volume.Status,
		}).Warn("Volume has no attachments but is not available, unreserving")

		err = volumes.Unreserve(ctx, c.BlockStorage, volume.ID).ExtractErr()
		if err != nil {
			return nil, err
		}
	}

	return c.WaitForVolumeAvailable(ctx, volumeID, timeout)
}

func (c *ClientSet) WaitForVolumeAttached(ctx context.Context, volumeID, serverID string, timeout time.Duration) (*volumes.Volume, error) {
	return c.waitForVolume(ctx, volumeID, timeout, fmt.Sprintf("in-use and attached to %s", serverID), func(volume *volumes.Volume) bool {
		return volume.Status == "in-use" && volumeAttachedTo(volume, serverID)
	})
}

func (c *ClientSet) waitForVolume(ctx context.Context, volumeID string, timeout time.Duration, desiredState string, ready func(*volumes.Volume) bool) (*volumes.Volume, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastVolume *volumes.Volume

	for {
		volume, err := volumes.Get(ctx, c.BlockStorage, volumeID).Extract()
		if err != nil {
			return nil, err
		}

		lastVolume = volume
		if ready(volume) {
			return volume, nil
		}

		log.WithFields(log.Fields{
			"volume_id":   volume.ID,
			"status":      volume.Status,
			"attachments": len(volume.Attachments),
			"desired":     desiredState,
		}).Debug("Waiting for volume state")

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for volume %s to become %s; last status=%s attachments=%d: %w", volumeID, desiredState, lastVolume.Status, len(lastVolume.Attachments), ctx.Err())
		case <-ticker.C:
		}
	}
}
