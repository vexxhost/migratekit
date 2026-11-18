package openstack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gosimple/slug"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

type OpenStackMetadata struct {
	UUID string `json:"uuid"`
}

func GetCurrentInstanceUUID(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://169.254.169.254/openstack/latest/meta_data.json", nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata service returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var metadata OpenStackMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return "", err
	}
	if metadata.UUID == "" {
		return "", fmt.Errorf("metadata service returned no instance UUID")
	}

	return metadata.UUID, nil
}

func VolumeName(vm *object.VirtualMachine, disk *types.VirtualDisk) string {
	return slug.Make(vm.Name() + "-" + strconv.Itoa(int(disk.Key)))
}

// ensuring backward compatibility
// TODO: remove
func VolumeNameOld(vm *object.VirtualMachine, disk *types.VirtualDisk) string {
	return slug.Make(vm.Name() + "-" + string(disk.DiskObjectId))
}
