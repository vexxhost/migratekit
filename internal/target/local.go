package target

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/vexxhost/migratekit/internal/vmware"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

type LocalDisk struct {
	VirtualMachine *object.VirtualMachine
	Disk           *types.VirtualDisk
	BasePath       string
}

type LocalDiskMetadata struct {
	ChangeID string `json:"change_id"`
	VMName   string `json:"vm_name"`
	DiskKey  int    `json:"disk_key"`
	Size     int64  `json:"size"`
}

func NewLocalDisk(ctx context.Context, vm *object.VirtualMachine, disk *types.VirtualDisk, basePath string) (*LocalDisk, error) {
	// Ensure base path exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base path %s: %w", basePath, err)
	}

	return &LocalDisk{
		VirtualMachine: vm,
		Disk:           disk,
		BasePath:       basePath,
	}, nil
}

func (t *LocalDisk) GetDisk() *types.VirtualDisk {
	return t.Disk
}

func (t *LocalDisk) Connect(ctx context.Context) error {
	// For local disk, we just ensure the target file exists
	targetPath := t.getTargetPath()
	dir := filepath.Dir(targetPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", dir, err)
	}

	// Create the target file if it doesn't exist
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		file, err := os.Create(targetPath)
		if err != nil {
			return fmt.Errorf("failed to create target file %s: %w", targetPath, err)
		}
		defer file.Close()

		// Pre-allocate space by seeking to the end
		if _, err := file.Seek(t.Disk.CapacityInBytes-1, 0); err != nil {
			return fmt.Errorf("failed to seek to end of file: %w", err)
		}
		if _, err := file.Write([]byte{0}); err != nil {
			return fmt.Errorf("failed to write to end of file: %w", err)
		}

		log.WithFields(log.Fields{
			"file": targetPath,
			"size": t.Disk.CapacityInBytes,
		}).Info("Created target file")
	}

	return nil
}

func (t *LocalDisk) GetPath(ctx context.Context) (string, error) {
	return t.getTargetPath(), nil
}

func (t *LocalDisk) Disconnect(ctx context.Context) error {
	// For local disk, no special disconnect needed
	return nil
}

func (t *LocalDisk) Exists(ctx context.Context) (bool, error) {
	targetPath := t.getTargetPath()
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (t *LocalDisk) GetCurrentChangeID(ctx context.Context) (*vmware.ChangeID, error) {
	metadataPath := t.getMetadataPath()
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return &vmware.ChangeID{}, nil
	}

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var metadata LocalDiskMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	if metadata.ChangeID == "" {
		return &vmware.ChangeID{}, nil
	}

	return vmware.ParseChangeID(metadata.ChangeID)
}

func (t *LocalDisk) WriteChangeID(ctx context.Context, changeID *vmware.ChangeID) error {
	metadataPath := t.getMetadataPath()

	metadata := LocalDiskMetadata{
		ChangeID: changeID.Value,
		VMName:   t.VirtualMachine.Name(),
		DiskKey:  int(t.Disk.Key),
		Size:     t.Disk.CapacityInBytes,
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Ensure metadata directory exists
	if err := os.MkdirAll(filepath.Dir(metadataPath), 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	log.WithFields(log.Fields{
		"metadata_file": metadataPath,
		"change_id":     changeID.Value,
	}).Info("Wrote change ID to metadata file")

	return nil
}

func (t *LocalDisk) getTargetPath() string {
	vmName := strings.ReplaceAll(t.VirtualMachine.Name(), "/", "_")
	diskKey := strconv.Itoa(int(t.Disk.Key))
	return filepath.Join(t.BasePath, vmName, fmt.Sprintf("disk-%s.raw", diskKey))
}

func (t *LocalDisk) getMetadataPath() string {
	vmName := strings.ReplaceAll(t.VirtualMachine.Name(), "/", "_")
	diskKey := strconv.Itoa(int(t.Disk.Key))
	return filepath.Join(t.BasePath, vmName, fmt.Sprintf("disk-%s.metadata.json", diskKey))
}
