package vmware_nbdkit

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/vexxhost/migratekit/internal/target"
	"github.com/vmware/govmomi/vim25/mo"
)

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
	script := `$ErrorActionPreference='Stop'
$remaining=@(` + strings.Join(quoted, ",") + `)
for ($attempt=0; $attempt -lt 60 -and $remaining.Count; $attempt++) {
 $disks=@(Get-Disk)
 $remaining=@(foreach ($id in $remaining) {
  $disk=$disks | Where-Object { $_.SerialNumber -and $_.SerialNumber.Trim() -eq $id }
  if (!$disk) { $id; continue }
  if ($disk.IsOffline) { $disk | Set-Disk -IsOffline $false }
  if ($disk.IsReadOnly) { Get-Disk -Number $disk.Number | Set-Disk -IsReadOnly $false }
 })
 if ($remaining.Count) { Start-Sleep -Seconds 2 }
}
if ($remaining.Count) { throw 'Converted volumes were not discovered during firstboot' }
`
	units := utf16.Encode([]rune(script))
	bytes := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(bytes[i*2:], unit)
	}
	return "powershell.exe -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(bytes)
}
