package process

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCanceledCommandDoesNotStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	marker := filepath.Join(t.TempDir(), "started")
	if err := CommandContext(ctx, "touch", marker).Run(); err == nil {
		t.Fatal("canceled command succeeded")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("canceled command started")
	}
}
func TestCancelStopsWorkerSubprocesses(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "child")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := CommandContext(ctx, "sh", "-c", `sleep 60 & echo $! > "$1"; wait`, "sh", pidfile)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var data []byte
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ = os.ReadFile(pidfile)
		if len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(data) == 0 {
		cancel()
		<-done
		t.Fatal("worker did not start")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled worker succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) {
			return
		}
		if err == nil {
			fields := strings.Fields(string(status)[strings.LastIndex(string(status), ")")+1:])
			if len(fields) > 0 && fields[0] == "Z" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker subprocess survived cancellation")
}
