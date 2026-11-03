package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
)

func TestSecondInstanceFailsBeforeTouchingTheStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	ep := runService(t, Config{Endpoint: channeltest.Endpoint(t), StateDir: dir, PeerCheck: allowAll})
	// Stands in for a write the first instance has not finished yet.
	pending := filepath.Join(dir, ".tmp-pending-write")
	if err := os.WriteFile(pending, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), quiet, Config{Endpoint: ep, StateDir: dir, PeerCheck: allowAll})
	if !errors.Is(err, localipc.ErrRunning) {
		t.Fatalf("second Run = %v, want %v", err, localipc.ErrRunning)
	}
	if _, err := os.Lstat(pending); err != nil {
		t.Fatalf("second instance touched the store: %v", err)
	}
}

func TestChannelIsReleasedWhenTheStoreCannotBeOpened(t *testing.T) {
	ep := channeltest.Endpoint(t)
	if err := Run(context.Background(), quiet, Config{Endpoint: ep, StateDir: "relative"}); err == nil {
		t.Fatal("Run accepted a relative state directory")
	}
	l, err := ep.Listen()
	if err != nil {
		t.Fatalf("channel still taken: %v", err)
	}
	_ = l.Close()
}
