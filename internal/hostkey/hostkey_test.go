package hostkey_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/hostkey"
	"github.com/castrowithcee/qatlas-deploy/internal/hostkey/hostkeytest"
)

func TestProbeReturnsTheServerKey(t *testing.T) {
	srv := hostkeytest.Start(t)
	got, err := hostkey.Probe(context.Background(), srv.Host, srv.Port)
	if err != nil {
		t.Fatal(err)
	}
	want := srv.Key()
	if got != want {
		t.Fatalf("Probe = %+v, want %+v", got, want)
	}
	if got.Type != "ssh-ed25519" || !strings.HasPrefix(got.Fingerprint, "SHA256:") ||
		!strings.HasPrefix(got.Authorized, "ssh-ed25519 ") {
		t.Fatalf("unexpected key forms: %+v", got)
	}
}

func TestProbeUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(l.Addr().String())
	_ = l.Close()
	if _, err := hostkey.Probe(context.Background(), host, port); !errors.Is(err, hostkey.ErrUnreachable) {
		t.Fatalf("got %v, want ErrUnreachable", err)
	}
}

func TestPattern(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"Git.Example.Test", "22"}:   "git.example.test",
		{"git.example.test", ""}:     "git.example.test",
		{"git.example.test", "2222"}: "[git.example.test]:2222",
	} {
		if got := hostkey.Pattern(in[0], in[1]); got != want {
			t.Errorf("Pattern(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestPinCheckAndConflict(t *testing.T) {
	srv := hostkeytest.Start(t)
	path := filepath.Join(t.TempDir(), "known_hosts")
	key := srv.Key()

	if st, err := hostkey.Check(path, srv.Host, srv.Port, key); err != nil || st != hostkey.Unknown {
		t.Fatalf("missing file: %v, %v", st, err)
	}
	if err := hostkey.Pin(path, srv.Host, srv.Port, key); err != nil {
		t.Fatal(err)
	}
	if err := hostkey.Pin(path, srv.Host, srv.Port, key); err != nil {
		t.Fatalf("pinning the same key again: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := srv.Pattern() + " " + key.Authorized + "\n"; string(data) != want {
		t.Fatalf("known_hosts = %q, want %q", data, want)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	if st, _ := hostkey.Check(path, srv.Host, srv.Port, key); st != hostkey.Pinned {
		t.Fatalf("state %v", st)
	}

	srv.Rekey(t)
	other := srv.Key()
	if st, _ := hostkey.Check(path, srv.Host, srv.Port, other); st != hostkey.Changed {
		t.Fatalf("state %v, want Changed", st)
	}
	if err := hostkey.Pin(path, srv.Host, srv.Port, other); !errors.Is(err, hostkey.ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("a conflicting pin changed the file")
	}
	// Another host in the same file is independent.
	if st, _ := hostkey.Check(path, "other.example.test", "22", other); st != hostkey.Unknown {
		t.Fatalf("other host: %v", st)
	}
}

func TestPinRefusesNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "known_hosts")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := hostkey.Pin(link, "git.example.test", "22", hostkeytest.Start(t).Key()); err == nil {
		t.Fatal("a symlinked known_hosts was accepted")
	}
}
