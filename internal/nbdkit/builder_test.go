package nbdkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildScopesVDDKLibrariesToChild(t *testing.T) {
	for _, parentPath := range []string{"", "/system/libraries"} {
		t.Run(parentPath, func(t *testing.T) {
			t.Setenv("LD_LIBRARY_PATH", parentPath)
			t.Setenv("MIGRATEKIT_TEST_INHERITED", "present")
			server, err := NewNbdkitBuilder().Build()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(filepath.Dir(server.socket)) })
			if got := os.Getenv("LD_LIBRARY_PATH"); got != parentPath {
				t.Fatalf("parent library path changed: got %q, want %q", got, parentPath)
			}
			env := make(map[string]string)
			for _, entry := range server.cmd.Environ() {
				key, value, _ := strings.Cut(entry, "=")
				env[key] = value
			}
			if got := env["LD_LIBRARY_PATH"]; got != "/usr/lib64/vmware-vix-disklib/lib64" {
				t.Fatalf("nbdkit library path: %q", got)
			}
			if env["MIGRATEKIT_TEST_INHERITED"] != "present" {
				t.Fatal("child did not inherit the parent environment")
			}
		})
	}
}
