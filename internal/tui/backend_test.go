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
		"stacks":     func() error { _, err := b.Stacks(ctx); return err },
		"check":      func() error { _, err := b.Check(ctx, ""); return err },
		"updates":    func() error { _, err := b.Updates(ctx, control.UpdatesArgs{}); return err },
		"runs":       func() error { _, err := b.Runs(ctx, ""); return err },
		"log":        func() error { _, err := b.Log(ctx, "20261030T140200Z-0000000c"); return err },
		"changelog":  func() error { _, err := b.Changelog(ctx, "demo-stack"); return err },
		"backups":    func() error { _, err := b.Backups(ctx, "demo-stack"); return err },
		"window":     func() error { _, err := b.Window(ctx); return err },
		"containers": func() error { _, err := b.Containers(ctx); return err },
		"container":  func() error { _, err := b.Container(ctx, "demo-app"); return err },
		// The service in this test has no stacks repository, so it refuses each of these before it changes anything.
		"deploy":  func() error { _, err := b.Deploy(ctx, "demo-stack"); return err },
		"update":  func() error { _, err := b.Update(ctx, []string{"demo-stack"}); return err },
		"pause":   func() error { _, err := b.Pause(ctx, "demo-stack"); return err },
		"unpause": func() error { _, err := b.Unpause(ctx, "demo-stack"); return err },
		"pin":     func() error { _, err := b.Pin(ctx, "demo-stack"); return err },
		"unpin":   func() error { _, err := b.Unpin(ctx, "demo-stack"); return err },
		"resume":  func() error { _, err := b.Resume(ctx, "demo-stack"); return err },
		// The settings calls get arguments the service refuses or answers harmlessly in its temporary state.
		"probe":         func() error { _, err := b.RepoProbe(ctx, "https://git.example.test/stacks.git"); return err },
		"reposet":       func() error { _, err := b.RepoSet(ctx, control.RepoSetArgs{}); return err },
		"fetch":         func() error { _, err := b.RepoFetch(ctx); return err },
		"keyshow":       func() error { _, err := b.KeyShow(ctx, ""); return err },
		"keygenerate":   func() error { _, err := b.KeyGenerate(ctx, "", false); return err },
		"keyset":        func() error { _, err := b.KeySet(ctx, "", "not-a-key", false); return err },
		"credentials":   func() error { _, err := b.Credentials(ctx); return err },
		"credentialset": func() error { return b.CredentialSet(ctx, "backend-test", "example-value") },
		"credentialdel": func() error { return b.CredentialDelete(ctx, "backend-test") },
		"windowset":     func() error { _, err := b.WindowSet(ctx, control.WindowSetArgs{}); return err },
		"notifications": func() error { _, err := b.Notifications(ctx); return err },
		"notifyadd":     func() error { _, err := b.NotifyAdd(ctx, control.NotifyAddArgs{}); return err },
		"notifyremove":  func() error { return b.NotifyRemove(ctx, "no-channel") },
		"notifytest":    func() error { _, err := b.NotifyTest(ctx, "no-channel"); return err },
		"secrets":       func() error { _, err := b.Secrets(ctx, "demo-stack"); return err },
		"secretset":     func() error { _, err := b.SecretSet(ctx, "demo-stack", "BACKEND_TEST", "example-value"); return err },
		"secretdelete":  func() error { _, err := b.SecretDelete(ctx, "demo-stack", "BACKEND_TEST"); return err },
		"restore":       func() error { _, err := b.BackupRestore(ctx, "demo-stack", "20261023T031000Z-4567cdef"); return err },
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
