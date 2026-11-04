package cli

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-deploy/internal/changelog"
	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

// testGit is the git layer of the next service a test starts; nil means the system git.
var testGit service.Git

// testRegistry configures the registry clients of the next service a test starts.
var testRegistry registry.Options

// testChangelog configures the GitHub client of the next service a test starts.
var testChangelog changelog.Options

// testDeployer is the deployer of the next service a test starts; nil means none.
var testDeployer engine.Deployer

// testBackups is the backup manager of the next service a test starts; nil means none.
var testBackups service.Backups

// testDocker reads the containers of the next service a test starts; nil means the engine at the default socket.
var testDocker service.Docker

func useEndpoint(t *testing.T, ep localipc.ServiceEndpoint) {
	t.Helper()
	old := serviceEndpoint
	serviceEndpoint = func() localipc.ServiceEndpoint { return ep }
	t.Cleanup(func() { serviceEndpoint = old })
}

func useStateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	old := stateDir
	stateDir = func() string { return dir }
	t.Cleanup(func() { stateDir = old })
	return dir
}

func startService(t *testing.T, check service.PeerCheck) {
	t.Helper()
	startServiceLogged(t, check, io.Discard)
}

// startServiceLogged starts the service with its log on logOut and returns once the channel exists.
func startServiceLogged(t *testing.T, check service.PeerCheck, logOut io.Writer) string {
	t.Helper()
	ep := channeltest.Endpoint(t)
	useEndpoint(t, ep)
	dir := useStateDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx, slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelDebug})),
			service.Config{Version: "9.9.9", Endpoint: ep, StateDir: dir, PeerCheck: check, Git: testGit, Deployer: testDeployer, Backups: testBackups, Registry: testRegistry, Changelog: testChangelog, Docker: testDocker})
	}()
	t.Cleanup(func() { cancel(); <-done })
	path, err := ep.Path()
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, err := os.Lstat(path); err == nil {
			return dir
		}
		if time.Now().After(deadline) {
			t.Fatal("service channel did not appear")
		}
	}
}

func TestStatusJSON(t *testing.T) {
	startService(t, func(net.Conn) error { return nil })
	code, stdout, stderr := run(t, "status", "--output", "json")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout, err)
	}
	started, err := time.Parse(time.RFC3339, got["started_at"].(string))
	if got["running"] != true || got["version"] != "9.9.9" || err != nil || time.Since(started) > time.Minute {
		t.Fatalf("status = %v", got)
	}
}

func TestStatusTable(t *testing.T) {
	startService(t, func(net.Conn) error { return nil })
	code, stdout, _ := run(t, "status")
	if code != 0 || !strings.Contains(stdout, "9.9.9") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
}

func TestStatusWithoutRunningService(t *testing.T) {
	useEndpoint(t, channeltest.Endpoint(t))
	code, stdout, stderr := run(t, "status")
	if code != ExitServiceNotRunning || stdout != "" ||
		!strings.Contains(stderr, "not running") || !strings.Contains(stderr, "unreachable") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestStatusWithoutServiceDirectory(t *testing.T) {
	ep := channeltest.Endpoint(t)
	ep.Dir += "/missing"
	useEndpoint(t, ep)
	if code, _, stderr := run(t, "status"); code != ExitServiceNotRunning || !strings.Contains(stderr, "not running") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}

func TestStatusAsNonRootNamesSudo(t *testing.T) {
	startService(t, func(net.Conn) error { return control.ErrNotRoot() })
	code, _, stderr := run(t, "status")
	if code != 1 || !strings.Contains(stderr, "sudo") || !strings.Contains(stderr, "permission") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}
