package tui

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

// Every method of the channel backend reaches a command the service knows.
func TestChannelBackendSpeaksTheServiceCommands(t *testing.T) {
	ep := channeltest.Endpoint(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), service.Config{Version: "9.9.9",
			Endpoint: ep, StateDir: filepath.Join(t.TempDir(), "state"), PeerCheck: func(net.Conn) error { return nil }})
	}()
	t.Cleanup(func() { cancel(); <-done })
	path, err := ep.Path()
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, err := os.Lstat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("service channel did not appear")
		}
	}

	b := NewBackend(ep)
	st, err := b.Status(ctx)
	if err != nil || st.Version != "9.9.9" {
		t.Fatalf("status = %+v, %v", st, err)
	}
	calls := map[string]func() error{
		"stacks":    func() error { _, err := b.Stacks(ctx); return err },
		"check":     func() error { _, err := b.Check(ctx, ""); return err },
		"updates":   func() error { _, err := b.Updates(ctx, control.UpdatesArgs{}); return err },
		"runs":      func() error { _, err := b.Runs(ctx, ""); return err },
		"log":       func() error { _, err := b.Log(ctx, "20261030T140200Z-0000000c"); return err },
		"changelog": func() error { _, err := b.Changelog(ctx, "demo-stack"); return err },
		"backups":   func() error { _, err := b.Backups(ctx, "demo-stack"); return err },
		"window":    func() error { _, err := b.Window(ctx); return err },
	}
	for name, call := range calls {
		var remote *control.Error
		if err := call(); errors.As(err, &remote) && (remote.Code == control.CodeUnknownCommand || remote.Code == control.CodeBadRequest) {
			t.Errorf("%s: %v", name, err)
		} else if err != nil && !errors.As(err, &remote) {
			t.Errorf("%s: no answer of the service: %v", name, err)
		}
	}
}

func TestChannelBackendWithoutService(t *testing.T) {
	_, err := NewBackend(channeltest.Endpoint(t)).Status(context.Background())
	if !errors.Is(err, client.ErrNotRunning) {
		t.Fatalf("err = %v", err)
	}
}
