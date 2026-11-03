package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/changelog"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
)

func startChangelog(t *testing.T) {
	t.Helper()
	reg := registrytest.New(t, registrytest.Config{
		Repos:  map[string][]string{"demo/app": {"1.2.3", "1.2.4", "1.3.0", "2.0.0"}},
		Labels: map[string]string{"org.opencontainers.image.source": "https://github.com/example-org/example-app"},
	})
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example-org/example-app/releases" {
			http.NotFound(w, r)
			return
		}
		var out []map[string]any
		for _, tag := range []string{"v1.3.0", "v1.2.4", "v1.2.3"} {
			out = append(out, map[string]any{"tag_name": tag, "name": "Release " + tag, "body": "\n\nFixes in " + tag + "\n- more",
				"html_url": "https://github.com/example-org/example-app/releases/tag/" + tag, "published_at": "2026-01-02T03:04:05Z"})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(gh.Close)
	oldReg, oldLog := testRegistry, testChangelog
	testRegistry = registry.Options{BaseURL: func(string) string { return reg.URL }, MinInterval: -1}
	testChangelog = changelog.Options{BaseURL: gh.URL, MinInterval: -1}
	t.Cleanup(func() { testRegistry, testChangelog = oldReg, oldLog })
	startBound(t, nil)
}

func TestChangelogCommandJSONAndTable(t *testing.T) {
	startChangelog(t)

	// Never checked: the stored detection is empty and nothing is asked.
	code, out, stderr := run(t, "stack", "changelog", "demo-stack", "--output", "json")
	if rows := rowsOf(t, out); code != 0 || len(rows) != 1 || rows[0]["status"] != "never-checked" {
		t.Fatalf("never checked: %d %s %s", code, out, stderr)
	}

	if code, _, stderr := run(t, "stack", "updates", "demo-stack", "--refresh"); code != 0 {
		t.Fatalf("refresh: %d %s", code, stderr)
	}
	code, out, stderr = run(t, "stack", "changelog", "demo-stack", "--output", "json")
	if code != 0 {
		t.Fatalf("changelog: %d %s", code, stderr)
	}
	rows := rowsOf(t, out)
	if len(rows) != 2 || rows[0]["version"] != "1.3.0" || rows[1]["version"] != "1.2.4" || rows[0]["status"] != "release" ||
		rows[0]["current"] != "1.2.3" || rows[0]["target"] != "1.3.0" || rows[0]["source"] != "example-org/example-app" ||
		rows[0]["variable"] != "DEMO_APP_VERSION" ||
		!strings.Contains(rows[0]["body"].(string), "Fixes in v1.3.0\n- more") {
		t.Fatalf("rows = %s", out)
	}
	if _, has := rows[0]["notes"]; has {
		t.Fatalf("json carries the full body, not notes: %s", out)
	}

	code, table, _ := run(t, "stack", "changelog", "demo-stack")
	if code != 0 || !strings.Contains(table, "NOTES") || !strings.Contains(table, "Fixes in v1.3.0") || strings.Contains(table, "- more") ||
		!strings.Contains(table, "Release v1.2.4") || strings.Contains(table, "BODY") {
		t.Fatalf("table: %d %q", code, table)
	}
}

func TestUpdatesChangelogFlag(t *testing.T) {
	startChangelog(t)
	code, out, stderr := run(t, "stack", "updates", "--refresh", "--changelog", "--output", "json")
	if code != 0 {
		t.Fatalf("updates --changelog: %d %s", code, stderr)
	}
	rows := rowsOf(t, out)
	var releases int
	for _, r := range rows {
		if r["stack"] == "demo-stack" && r["status"] == "release" {
			releases++
		}
	}
	if releases != 2 || rowFor(rows, "plain-stack")["status"] != "no-updates" || rowFor(rows, "broken-stack")["status"] != "unreadable" {
		t.Fatalf("rows = %s", out)
	}
	if code, table, _ := run(t, "stack", "updates", "--changelog"); code != 0 || !strings.Contains(table, "NOTES") || !strings.Contains(table, "no-updates") {
		t.Fatalf("table: %d %q", code, table)
	}
}

func TestChangelogUnknownStack(t *testing.T) {
	startChangelog(t)
	if code, _, stderr := run(t, "stack", "changelog", "nope"); code == 0 || !strings.Contains(stderr, "no such stack") {
		t.Fatalf("unknown: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "changelog", "broken-stack"); code == 0 || !strings.Contains(stderr, "cannot be read") {
		t.Fatalf("unreadable: %d %q", code, stderr)
	}
	if code, _, _ := run(t, "stack", "changelog"); code == 0 {
		t.Fatal("a missing stack argument must fail")
	}
}
