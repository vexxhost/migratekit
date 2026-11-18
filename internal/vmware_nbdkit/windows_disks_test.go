package vmware_nbdkit

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/vexxhost/migratekit/internal/target"
)

func TestWindowsOnlineDisksUsesOnlyConversionSerials(t *testing.T) {
	command := windowsOnlineDisksCommand([]string{"disk-one", "disk'two"})
	if !strings.HasPrefix(command, "powershell.exe -NoProfile -NonInteractive -Command ") {
		t.Fatal("missing noninteractive PowerShell invocation")
	}
	_, payload, ok := strings.Cut(command, "FromBase64String('")
	if !ok {
		t.Fatal("missing compressed script")
	}
	payload, _, ok = strings.Cut(payload, "')")
	if !ok {
		t.Fatal("unterminated compressed script")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	script := string(decoded)
	for _, required := range []string{"'disk-one'", "'disk''two'", "Get-PhysicalDisk", "SerialNumber.Trim() -eq $id", "EnsureOnline([uint32]$disk[0].DeviceID,$id)", "String.Equals(Serial(handle),expected,StringComparison.OrdinalIgnoreCase)", "BitConverter.GetBytes((ulong)3)", "input[4]=1"} {
		if !strings.Contains(script, required) {
			t.Errorf("missing serial-scoped operation %q", required)
		}
	}
	if strings.Contains(script, "FriendlyName") {
		t.Fatal("disk names cannot identify all VirtIO SCSI disks")
	}
	if strings.Contains(script, "__MIGRATEKIT_VOLUME_SERIALS__") {
		t.Fatal("volume identities were not substituted")
	}
}

func TestWindowsOnlineDisksCommandFitsBatchLimit(t *testing.T) {
	var ids []string
	for i := 0; i < 64; i++ {
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprint(i))))
		ids = append(ids, digest[:8]+"-"+digest[8:12]+"-"+digest[12:16]+"-"+digest[16:20]+"-"+digest[20:32])
	}
	if length := len(windowsOnlineDisksCommand(ids)); length > 8191 {
		t.Fatalf("firstboot command exceeds cmd.exe's line limit: %d", length)
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
