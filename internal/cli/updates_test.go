package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
)

func startUpdates(t *testing.T) {
	t.Helper()
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": {"1.2.3", "1.2.4", "1.3.0", "2.0.0", "2.1.0-rc.1"}}})
	old := testRegistry
	testRegistry = registry.Options{BaseURL: func(string) string { return srv.URL }, MinInterval: -1}
	t.Cleanup(func() { testRegistry = old })
	startBound(t, nil)
}

func rowsOf(t *testing.T, out string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("output = %q: %v", out, err)
	}
	return rows
}

func rowFor(rows []map[string]any, stack string) map[string]any {
	for _, r := range rows {
		if r["stack"] == stack {
			return r
		}
	}
	return nil
}

func TestUpdatesNeverCheckedRefreshAndStored(t *testing.T) {
	startUpdates(t)
	code, out, stderr := run(t, "stack", "updates", "--output", "json")
	if code != 0 {
		t.Fatalf("updates: %d %s", code, stderr)
	}
	rows := rowsOf(t, out)
	for _, name := range []string{"demo-stack", "plain-stack"} {
		if r := rowFor(rows, name); r == nil || r["status"] != "never-checked" {
			t.Fatalf("%s = %v in %s", name, r, out)
		}
	}
	if r := rowFor(rows, "broken-stack"); r == nil || r["status"] != "unreadable" || r["error"] == "" {
		t.Fatalf("broken-stack = %v", r)
	}

	code, out, stderr = run(t, "stack", "updates", "demo-stack", "--refresh", "--output", "json")
	if code != 0 {
		t.Fatalf("refresh: %d %s", code, stderr)
	}
	rows = rowsOf(t, out)
	if len(rows) != 1 || rows[0]["variable"] != "DEMO_APP_VERSION" || rows[0]["current"] != "1.2.3" || rows[0]["available"] != "1.3.0" ||
		rows[0]["major_available"] != "2.0.0" || rows[0]["status"] != "update" || rows[0]["checked"] == nil {
		t.Fatalf("refresh = %s", out)
	}

	code, stored, _ := run(t, "stack", "updates", "demo-stack", "--output", "json")
	if code != 0 || stored != out {
		t.Fatalf("stored = %q, want %q", stored, out)
	}
	code, table, _ := run(t, "stack", "updates")
	if code != 0 || !strings.Contains(table, "MAJOR_AVAILABLE") || !strings.Contains(table, "1.3.0") || !strings.Contains(table, "never-checked") {
		t.Fatalf("table: %d %q", code, table)
	}
}

func TestUpdatesUnknownAndUnreadableStack(t *testing.T) {
	startUpdates(t)
	if code, _, stderr := run(t, "stack", "updates", "nope", "--refresh"); code == 0 || !strings.Contains(stderr, "no such stack") || !strings.Contains(stderr, "not-found") {
		t.Fatalf("unknown: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "updates", "broken-stack"); code == 0 || !strings.Contains(stderr, "cannot be read") {
		t.Fatalf("unreadable: %d %q", code, stderr)
	}
}
