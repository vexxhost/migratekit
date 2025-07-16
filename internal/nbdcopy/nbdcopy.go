package nbdcopy

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/francoisovh/migratekit/internal/progress"
	log "github.com/sirupsen/logrus"
)

func Run(source, destination string, size int64, targetIsClean bool, ctx context.Context) error {
	logger := log.WithFields(log.Fields{
		"source":      source,
		"destination": destination,
	})

	progressRead, progressWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer progressRead.Close()

	args := []string{
		"--progress=3",
		source,
		destination,
	}

	if targetIsClean {
		args = append(args, "--destination-is-zero")
	}

	cmd := exec.Command(
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

	jobIDVal := ctx.Value("jobID")
	jobID, ok := jobIDVal.(string)
	if !ok {
		log.Warn("jobID missing or not a string in context")
		jobID = "unknown"
	}
	pb := progress.NewDataProgressReporter("Full copy", size, nil, jobID)
	go func() {
		scanner := bufio.NewScanner(progressRead)
		for scanner.Scan() {
			progressParts := strings.Split(scanner.Text(), "/")
			progressVal, err := strconv.ParseInt(progressParts[0], 10, 64)
			if err != nil {
				log.Error("Error parsing progress: ", err)
				continue
			}

			pct := progressVal
			pb.Bar().Set64(pct * size / 100)

			if pb.Reporter() != nil {
				pb.Reporter().Percent(int(pct), "Copying data")
			}
		}

		if err := scanner.Err(); err != nil {
			log.Error("Error reading progress: ", err)
		}
	}()

	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}
