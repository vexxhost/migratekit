package vmware_nbdkit

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/vexxhost/migratekit/internal/target"
)

func TestWindowsOnlineDisksUsesOnlyConversionSerials(t *testing.T) {
	command := windowsOnlineDisksCommand([]string{"disk-one", "disk'two"})
	prefix := "powershell.exe -NoProfile -NonInteractive -EncodedCommand "
	if !strings.HasPrefix(command, prefix) {
		t.Fatal("missing noninteractive PowerShell invocation")
	}
	bytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, prefix))
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(bytes)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(bytes[i*2:])
	}
	script := string(utf16.Decode(units))
	for _, required := range []string{"'disk-one'", "'disk''two'", "SerialNumber.Trim() -eq $id", "Set-Disk -IsOffline $false", "Set-Disk -IsReadOnly $false"} {
		if !strings.Contains(script, required) {
			t.Errorf("missing serial-scoped operation %q", required)
		}
	}
	if strings.Contains(script, "FriendlyName") {
		t.Fatal("disk names cannot identify all VirtIO SCSI disks")
	}
}

func TestWindowsDiskFirstbootSkippedForDefaultBus(t *testing.T) {
	for _, bus := range []string{"", "virtio"} {
		ctx := context.WithValue(context.Background(), "volumeCreateOpts", &target.VolumeCreateOpts{BusType: bus})
		command, err := (&NbdkitServers{}).windowsDiskFirstboot(ctx, nil)
		if err != nil || command != "" {
			t.Fatalf("unexpected firstboot for %q: %s %v", bus, command, err)
		}
	}
}
