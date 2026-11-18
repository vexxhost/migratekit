package vmware_nbdkit

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/vexxhost/migratekit/internal/nbdkit"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

type signalTarget struct {
	copyTarget
	detachErr error
	calls     atomic.Int32
	marker    string
}

func (d *signalTarget) Disconnect(context.Context) error {
	d.calls.Add(1)
	if d.marker != "" {
		f, err := os.OpenFile(d.marker, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		_, err = f.WriteString("disconnect\n")
		f.Close()
		if err != nil {
			return err
		}
	}
	return d.detachErr
}
func copyTestServer(t *testing.T, d *signalTarget) *NbdkitServer {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nbdcopy"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	nbd, err := nbdkit.NewNbdkitBuilder().Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(nbd.Socket())) })
	vm := object.NewVirtualMachine(nil, types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-test"})
	vm.InventoryPath = "/dc/vm/test-guest"
	return &NbdkitServer{Servers: &NbdkitServers{VirtualMachine: vm}, Disk: d.GetDisk(), Nbdkit: nbd}
}
func TestCompletedSyncReleasesSignalHandler(t *testing.T) {
	if os.Getenv("MIGRATEKIT_TEST_SIGNAL_CHILD") == "1" {
		destination := &signalTarget{marker: os.Getenv("MIGRATEKIT_TEST_SIGNAL_MARKER")}
		server := copyTestServer(t, destination)
		if err := server.SyncToTarget(context.Background(), destination); err != nil {
			t.Fatal(err)
		}
		current := make(chan os.Signal, 1)
		signal.Notify(current, syscall.SIGTERM)
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		<-current
		time.Sleep(250 * time.Millisecond)
		os.Exit(0)
	}
	marker := filepath.Join(t.TempDir(), "detach-calls")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCompletedSyncReleasesSignalHandler$")
	child.Env = append(os.Environ(), "MIGRATEKIT_TEST_SIGNAL_CHILD=1", "MIGRATEKIT_TEST_SIGNAL_MARKER="+marker)
	output, err := child.CombinedOutput()
	data, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatalf("signal test did not reach completed copy: %v; %s", readErr, output)
	}
	count := strings.Count(string(data), "disconnect\n")
	if err != nil || count != 1 {
		t.Fatalf("finished copy retained an active signal handler: err=%v detach calls=%d; child output=%s", err, count, output)
	}
}

func TestCleanupContextSurvivesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), "cleanup-test", "preserved"))
	cancel()
	cleanup, stop := cleanupContext(ctx)
	defer stop()
	if cleanup.Err() != nil || cleanup.Value("cleanup-test") != "preserved" {
		t.Fatal("cleanup lost its context values or inherited cancellation")
	}
	if deadline, ok := cleanup.Deadline(); !ok || deadline.After(time.Now().Add(2*time.Minute+time.Second)) {
		t.Fatal("cleanup has no bounded deadline")
	}
}
