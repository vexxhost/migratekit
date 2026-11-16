package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// CommandContext stops the entire worker group when its context is canceled.
// Conversion workers may create subprocesses that also hold disk devices open.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return cmd
}
