package target

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindDeviceBeforeFirstAttachment(t *testing.T) {
	root := t.TempDir()
	deviceDir := filepath.Join(root, "by-id")
	volumeID := "12345678-1234-1234-1234-123456789abc"
	path, err := findDeviceInDir(deviceDir, volumeID)
	if err != nil || path != "" {
		t.Fatalf("unattached volume: path=%q, err=%v", path, err)
	}

	if err := os.Mkdir(deviceDir, 0755); err != nil {
		t.Fatal(err)
	}
	device := filepath.Join(root, "vdb")
	if err := os.WriteFile(device, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(device, filepath.Join(deviceDir, "virtio-"+volumeID[:18])); err != nil {
		t.Fatal(err)
	}
	path, err = findDeviceInDir(deviceDir, volumeID)
	if err != nil || path != device {
		t.Fatalf("attached volume: path=%q, err=%v", path, err)
	}
	path, err = findDeviceInDir(deviceDir, "abcdefab-1234-1234-1234-123456789abc")
	if err != nil || path != "" {
		t.Fatalf("unrelated volume: path=%q, err=%v", path, err)
	}
}

func TestFindDeviceReadError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := findDeviceInDir(file, "12345678-1234-1234-1234-123456789abc"); err == nil {
		t.Fatal("expected directory read error")
	}
}
