package vmware_nbdkit

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/vexxhost/migratekit/internal/target"
	"github.com/vmware/govmomi/vim25/mo"
)

//go:embed windows_disks.ps1
var windowsOnlineDisksScript string

// The conversion tool's default firstboot hook selects disks by a VirtIO name.
// VirtIO SCSI disks can instead have a QEMU name. Select only the Cinder serials
// belonging to this conversion so that unrelated SAN disks keep their policy.
func (s *NbdkitServers) windowsDiskFirstboot(ctx context.Context, targets []*migrationTarget) (string, error) {
	opts, _ := ctx.Value("volumeCreateOpts").(*target.VolumeCreateOpts)
	if opts == nil || opts.BusType != "scsi" {
		return "", nil
	}
	var vm mo.VirtualMachine
	if err := s.VirtualMachine.Properties(ctx, s.VirtualMachine.Reference(), []string{"config.guestId"}, &vm); err != nil {
		return "", err
	}
	if !strings.HasPrefix(strings.ToLower(vm.Config.GuestId), "win") {
		return "", nil
	}
	ids := make([]string, 0, len(targets))
	for _, mt := range targets {
		destination, ok := mt.Target.(*target.OpenStack)
		if !ok {
			return "", fmt.Errorf("Windows disk setup requires OpenStack volume identities")
		}
		volume, err := destination.ClientSet.GetVolumeForDisk(ctx, destination.VirtualMachine, destination.Disk)
		if err != nil {
			return "", err
		}
		ids = append(ids, volume.ID)
	}
	return windowsOnlineDisksCommand(ids), nil
}

func windowsOnlineDisksCommand(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "'" + strings.ReplaceAll(id, "'", "''") + "'"
	}
	script := strings.Replace(windowsOnlineDisksScript, "__MIGRATEKIT_VOLUME_SERIALS__", strings.Join(quoted, ","), 1)
	// Windows runs firstboot commands through cmd.exe. Compress the script to
	// keep its command line below cmd.exe's limit, including larger disk sets.
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(script)) // bytes.Buffer writes cannot fail.
	_ = writer.Close()
	payload := base64.StdEncoding.EncodeToString(compressed.Bytes())
	return `powershell.exe -NoProfile -NonInteractive -Command "$m=New-Object IO.MemoryStream(,[Convert]::FromBase64String('` + payload +
		`'));$z=New-Object IO.Compression.GZipStream($m,[IO.Compression.CompressionMode]::Decompress);$r=New-Object IO.StreamReader($z);try{Invoke-Expression ($r.ReadToEnd())}finally{$r.Dispose()}"`
}
