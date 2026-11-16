package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
)

func TestPrepareMigrationSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name                                    string
		allow, answer, promptError, wantRemoved bool
	}{
		{"explicit recovery", true, false, false, true},
		{"interactive confirmation", false, true, false, true},
		{"declined", false, false, false, false},
		{"noninteractive refusal", false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			model := simulator.VPX()
			model.Machine = 1
			if err := model.Create(); err != nil {
				t.Fatal(err)
			}
			defer model.Remove()
			api := model.Service.NewServer()
			defer api.Close()
			client, err := govmomi.NewClient(ctx, api.URL, true)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Logout(ctx)
			finder := find.NewFinder(client.Client, true)
			dc, err := finder.DefaultDatacenter(ctx)
			if err != nil {
				t.Fatal(err)
			}
			finder.SetDatacenter(dc)
			vms, err := finder.VirtualMachineList(ctx, "*")
			if err != nil || len(vms) == 0 {
				t.Fatalf("VM fixture: %v", err)
			}
			vm := vms[0]
			task, err := vm.CreateSnapshot(ctx, "migratekit", "Ephemeral snapshot for MigrateKit", false, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = task.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			// A separate snapshot must survive removal of the migration snapshot.
			task, err = vm.CreateSnapshot(ctx, "retained-backup", "unrelated snapshot", false, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = task.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			promptCalls := 0
			err = prepareMigrationSnapshot(ctx, vm, tc.allow, func() (bool, error) {
				promptCalls++
				if tc.promptError {
					return false, errors.New("explicit recovery required")
				}
				return tc.answer, nil
			})
			if tc.wantRemoved {
				if err != nil {
					t.Fatal(err)
				}
				if ref, _ := vm.FindSnapshot(ctx, "migratekit"); ref != nil {
					t.Fatal("returned before snapshot removal completed")
				}
			} else {
				if err == nil {
					t.Fatal("unapproved removal succeeded")
				}
				if ref, _ := vm.FindSnapshot(ctx, "migratekit"); ref == nil {
					t.Fatal("unapproved removal changed snapshot")
				}
			}
			if ref, err := vm.FindSnapshot(ctx, "retained-backup"); err != nil || ref == nil {
				t.Fatal("unrelated child snapshot removed")
			}
			if tc.allow && promptCalls != 0 {
				t.Fatal("explicit recovery unexpectedly prompted")
			}
		})
	}
}

func TestSnapshotRecoveryFlagIsExplicit(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("delete-existing-snapshot")
	if flag == nil || flag.DefValue != "false" {
		t.Fatal("snapshot removal must require explicit opt in")
	}
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = input
	t.Cleanup(func() {
		os.Stdin = previous
		input.Close()
		output.Close()
	})
	if accepted, err := confirmSnapshotRemoval(); accepted || err == nil || !strings.Contains(err.Error(), "--delete-existing-snapshot") {
		t.Fatal("missing actionable noninteractive recovery error")
	}
}
