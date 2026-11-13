package vmware_nbdkit

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vexxhost/migratekit/internal/target"
)

func TestConversionPreparesRequestedStorageDriver(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nwhile [ $# -gt 0 ]; do\nif [ \"$1\" = --block-driver ]; then printf '%s' \"$2\"; exit 0; fi\nshift\ndone\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "virt-v2v-in-place"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	for _, test := range []struct{ bus, driver string }{{"virtio", "virtio-blk"}, {"scsi", "virtio-scsi"}} {
		t.Run(test.bus, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), "volumeCreateOpts", &target.VolumeCreateOpts{BusType: test.bus})
			cmd := conversionCommand(ctx, true, "-i", "libvirtxml", "/tmp/guest.xml")
			output, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != test.driver {
				t.Fatalf("prepared %q for %s disk bus", output, test.bus)
			}
			if !reflect.DeepEqual(cmd.Args[1:3], []string{"-v", "-x"}) {
				t.Fatal("debug flags lost")
			}
			if !reflect.DeepEqual(cmd.Args[len(cmd.Args)-3:], []string{"-i", "libvirtxml", "/tmp/guest.xml"}) {
				t.Fatal("source arguments lost")
			}
		})
	}
}

func TestConversionForwardsGuestNetworkConfigurations(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "virt-v2v-in-place"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	mappings := []string{
		"00:11:22:33:44:55:ip:192.0.2.10,192.0.2.1,24,192.0.2.53",
		"00:11:22:33:44:66:ip:198.51.100.10,,24",
	}
	ctx := context.WithValue(context.Background(), "guestNetworkConfigs", mappings)
	output, err := conversionCommand(ctx, false, "-i", "libvirtxml", "/tmp/guest.xml").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--block-driver", "virtio-blk", "--mac", mappings[0], "--mac", mappings[1], "-i", "libvirtxml", "/tmp/guest.xml"}
	if !reflect.DeepEqual(strings.Split(strings.TrimSuffix(string(output), "\n"), "\n"), want) {
		t.Fatalf("converter received unexpected arguments: %q", output)
	}
}
