package cli

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
)

func startUpdate(t *testing.T, dep engine.Deployer) {
	t.Helper()
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": {"1.2.3", "1.2.4", "1.3.0", "2.0.0"}}})
	old := testRegistry
	testRegistry = registry.Options{BaseURL: func(string) string { return srv.URL }, MinInterval: -1}
	t.Cleanup(func() { testRegistry = old })
	startBound(t, dep)
	settle(t)
}

func TestUpdateCommand(t *testing.T) {
	startUpdate(t, okDeployer{})
	if code, _, stderr := run(t, "stack", "update"); code == 0 || !strings.Contains(stderr, "at least one stack") {
		t.Fatalf("no stack: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "update", "demo-stack", "--version", "1.2.3"); code == 0 || !strings.Contains(stderr, "not higher") {
		t.Fatalf("equal version: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "update", "plain-stack"); code == 0 || !strings.Contains(stderr, "never updated") {
		t.Fatalf("no rules: %d %q", code, stderr)
	}

	code, out, stderr := run(t, "stack", "update", "demo-stack", "--output", "json")
	if code != 0 {
		t.Fatalf("update: %d %s", code, stderr)
	}
	rows := rowsOf(t, out)
	if len(rows) != 1 || rows[0]["stack"] != "demo-stack" || rows[0]["variable"] != "DEMO_APP_VERSION" || rows[0]["from"] != "1.2.3" ||
		rows[0]["to"] != "1.3.0" || rows[0]["result"] != "succeeded" || rows[0]["run"] == "" || rows[0]["commit"] == "" {
		t.Fatalf("rows = %s", out)
	}
	code, out, _ = run(t, "stack", "update", "demo-stack")
	if code != 0 || !strings.Contains(out, "current") {
		t.Fatalf("again: %d %q", code, out)
	}
}

func TestUpdateFailedStepExitsWithOne(t *testing.T) {
	dep := &checkDeployer{}
	startUpdate(t, dep)
	dep.fail.Store(true)
	code, out, stderr := run(t, "stack", "update", "demo-stack", "--version", "2.0.0")
	if code != 1 || !strings.Contains(stderr, "1 of 1 stack(s) were not updated") || !strings.Contains(out, "skipped") {
		t.Fatalf("failed step: %d %q %q", code, out, stderr)
	}
}
