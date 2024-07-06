package nbdkit

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	log "github.com/sirupsen/logrus"
)

type NbdkitServer struct {
	cmd     *exec.Cmd
	socket  string
	pidFile string
}

func (s *NbdkitServer) Start() error {
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	log.Debug("Running command: ", s.cmd)
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start nbdkit server: %w", err)
	}

	pidFileTimeout := time.After(10 * time.Second)
	tick := time.Tick(100 * time.Millisecond)

	for {
		select {
		case <-pidFileTimeout:
			s.cmd.Process.Kill()
			return fmt.Errorf("timeout waiting for pidfile to appear: %s", s.pidFile)
		case <-tick:
			if _, err := os.Stat(s.pidFile); err == nil {
				return nil
			}
		}
	}
}

func (s *NbdkitServer) Stop() error {
	if err := s.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to stop nbdkit server: %w", err)
	}

	os.Remove(s.socket)
	return nil
}

func (s *NbdkitServer) Socket() string {
	return s.socket
}

func (s *NbdkitServer) LibNBDExportName() string {
	return fmt.Sprintf("nbd+unix:///?socket=%s", s.socket)
}
