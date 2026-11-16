package nbdkit

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

func TestStartDoesNotLogPassword(t *testing.T) {
	var output bytes.Buffer
	logger := log.StandardLogger()
	oldOutput, oldLevel := logger.Out, logger.GetLevel()
	logger.SetOutput(&output)
	logger.SetLevel(log.DebugLevel)
	t.Cleanup(func() { logger.SetOutput(oldOutput); logger.SetLevel(oldLevel) })
	secret := "test-password-not-for-logs"
	dir := t.TempDir()
	server := &NbdkitServer{
		cmd:     exec.Command("true", "password="+secret),
		socket:  filepath.Join(dir, "nbdkit.sock"),
		pidFile: filepath.Join(dir, "nbdkit.pid"),
	}
	if err := os.WriteFile(server.pidFile, []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	if err := server.cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("debug logs contain VMware password")
	}
	if !strings.Contains(output.String(), server.socket) {
		t.Fatal("debug log should identify the nbdkit socket")
	}
}
