package vmware_nbdkit

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLUKSKeyFilesRemainFileReferences(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "unlock key=one")
	const secret = "example-passphrase-not-an-argument"
	if err := os.WriteFile(filename, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	values := []string{"all=" + filename, "/dev/sda2=" + filename, "urn:uuid:12345678-1234-1234-1234-123456789abc=" + filename}
	keys, err := LUKSKeyFileArgs(values)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), "luksKeys", keys)
	args := conversionCommand(ctx, false, "-i", "libvirtxml", "guest.xml").Args
	want := []string{"virt-v2v-in-place", "--block-driver", "virtio-blk", "--key", "all:file:" + filename, "--key", "/dev/sda2:file:" + filename, "--key", "12345678-1234-1234-1234-123456789abc:file:" + filename, "-i", "libvirtxml", "guest.xml"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected converter arguments: %q", args)
	}
	if strings.Contains(strings.Join(args, " "), secret) {
		t.Fatal("passphrase was exposed in converter arguments")
	}
}

func TestLUKSKeyFileValidation(t *testing.T) {
	dir := t.TempDir()
	for _, value := range []string{"", "all", "all=", "=" + dir, "inline:key:value=" + dir, "all=" + dir, "all=" + filepath.Join(dir, "missing")} {
		t.Run(value, func(t *testing.T) {
			if _, err := LUKSKeyFileArgs([]string{value}); err == nil {
				t.Fatal("invalid key specification was accepted")
			}
		})
	}
	if keys, err := LUKSKeyFileArgs(nil); err != nil || len(keys) != 0 {
		t.Fatalf("default conversion should require no keys: %q %v", keys, err)
	}
}
