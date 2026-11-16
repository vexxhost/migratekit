package nbdcopy

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/vexxhost/migratekit/internal/process"
	"github.com/vexxhost/migratekit/internal/progress"
)

func Run(ctx context.Context, source, destination string, size int64, targetIsClean bool) error {
	logger := log.WithFields(log.Fields{
		"source":      source,
		"destination": destination,
	})

	progressRead, progressWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer progressRead.Close()
	defer progressWrite.Close()

	args := []string{
		"--progress=3",
		source,
		destination,
	}

	if targetIsClean {
		args = append(args, "--destination-is-zero")
	}

	cmd := process.CommandContext(ctx,
		"nbdcopy",
		args...,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{progressWrite}

	logger.Debug("Running command: ", cmd)
	if err := cmd.Start(); err != nil {
		return err
	}

	// Close the parent's copy of progressWrite
	// See: https://github.com/golang/go/issues/4261
	progressWrite.Close()

	bar := progress.DataProgressBar("Full copy", size)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(progressRead)
		for scanner.Scan() {
			progressParts := strings.Split(scanner.Text(), "/")
			progress, err := strconv.ParseInt(progressParts[0], 10, 64)
			if err != nil {
				log.Error("Error parsing progress: ", err)
				continue
			}

			bar.Set64(progress * size / 100)
		}

		if err := scanner.Err(); err != nil {
			log.Error("Error reading progress: ", err)
		}
	}()

	err = cmd.Wait()
	<-done
	return err
}
