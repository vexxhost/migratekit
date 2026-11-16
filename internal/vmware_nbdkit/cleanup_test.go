package vmware_nbdkit

import (
	"context"
	"errors"
	"testing"
)

func TestSyncReturnsDetachError(t *testing.T) {
	want := errors.New("detach failed")
	destination := &signalTarget{detachErr: want}
	server := copyTestServer(t, destination)
	if err := server.SyncToTarget(context.Background(), destination); !errors.Is(err, want) {
		t.Fatalf("detach failure lost: %v", err)
	}
	if destination.calls.Load() != 1 {
		t.Fatal("expected one detach")
	}
}
func TestSyncJoinsCopyAndDetachErrors(t *testing.T) {
	want := errors.New("detach failed")
	destination := &signalTarget{detachErr: want}
	server := copyTestServer(t, destination)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := server.SyncToTarget(ctx, destination)
	if !errors.Is(err, want) || !errors.Is(err, context.Canceled) {
		t.Fatalf("copy or cleanup failure lost: %v", err)
	}
}

type failedConnectTarget struct {
	signalTarget
	connectErr error
}

func (t *failedConnectTarget) Connect(context.Context) error { return t.connectErr }
func TestSyncCleansUpAfterConnectionFailure(t *testing.T) {
	want := errors.New("attachment incomplete")
	destination := &failedConnectTarget{connectErr: want}
	server := copyTestServer(t, &destination.signalTarget)
	if err := server.SyncToTarget(context.Background(), destination); !errors.Is(err, want) {
		t.Fatalf("connection failure lost: %v", err)
	}
	if destination.calls.Load() != 1 {
		t.Fatal("partial attachment was not cleaned up")
	}
}
