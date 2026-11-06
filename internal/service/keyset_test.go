package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func TestKeySetAnswersAndNeverEchoesTheKey(t *testing.T) {
	var logBuf lockedBuffer
	ep := channeltest.Endpoint(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		done <- Run(ctx, log, Config{Endpoint: ep, StateDir: filepath.Join(t.TempDir(), "state"), PeerCheck: allowAll})
	}()
	t.Cleanup(func() { cancel(); <-done })
	waitForChannel(t, ep)
	cl, err := client.Dial(context.Background(), ep)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	key := testKeyPEM(t, "rsa", "")
	body := strings.Split(strings.TrimSpace(key), "\n")[1]
	var res control.KeyResult
	if err := cl.Call(context.Background(), control.KeySetCommand, control.KeySetArgs{Key: key}, &res); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.PublicKey, "ssh-rsa ") || !strings.HasPrefix(res.Fingerprint, "SHA256:") {
		t.Fatalf("result = %+v", res)
	}
	var shown control.KeyResult
	if err := cl.Call(context.Background(), control.KeyShowCommand, nil, &shown); err != nil || shown != res {
		t.Fatalf("show = %+v, %v", shown, err)
	}
	for command, key := range map[string]string{"again": key, "passphrase": testKeyPEM(t, "ed25519", "demo-passphrase"), "garbage": "not a key " + body} {
		var ce *control.Error
		err := cl.Call(context.Background(), control.KeySetCommand, control.KeySetArgs{Key: key}, nil)
		if !errors.As(err, &ce) {
			t.Fatalf("%s: err = %v", command, err)
		}
		want := control.CodeInvalid
		if command == "again" {
			want = control.CodeKeyExists
		}
		if ce.Code != want || strings.Contains(ce.Message, body) {
			t.Fatalf("%s: %v", command, ce)
		}
	}
	if strings.Contains(logBuf.String(), body) {
		t.Fatal("the key appears in the log")
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitForChannel(t *testing.T, ep localipc.ServiceEndpoint) {
	t.Helper()
	path, err := ep.Path()
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, err := os.Lstat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("service channel did not appear")
		}
	}
}
