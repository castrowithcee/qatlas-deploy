package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/service/servicetest"
)

func startSettingsService(t *testing.T) *repoEnv {
	t.Helper()
	e := startRepoService(t)
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint); code != 0 {
		t.Fatalf("repo set: %d %s", code, stderr)
	}
	return e
}

func settingsOfOutput(t *testing.T, out string) map[string]any {
	t.Helper()
	s, ok := objectOf(t, out)["settings"].(map[string]any)
	if !ok {
		t.Fatalf("no settings in %q", out)
	}
	return s
}

func TestStackSettingsShow(t *testing.T) {
	startSettingsService(t)
	code, out, stderr := run(t, "stack", "settings", "demo-stack", "--output", "json")
	if code != 0 {
		t.Fatalf("show: %d %s", code, stderr)
	}
	if got := objectOf(t, out); got["stack"] != "demo-stack" || got["configured"] != true {
		t.Fatalf("show = %v", got)
	}
	s := settingsOfOutput(t, out)
	update, _ := s["update"].(map[string]any)
	check, _ := s["check"].(map[string]any)
	if s["version"] != float64(1) || update["mode"] != "automatic" || update["ceiling"] != "minor" || update["major"] != "stepwise" ||
		check["containers"] != true {
		t.Fatalf("settings = %v", s)
	}

	code, out, _ = run(t, "stack", "settings", "plain-stack", "--output", "json")
	if got := objectOf(t, out); code != 0 || got["configured"] != false || got["settings"] != nil {
		t.Fatalf("plain: %d %v", code, got)
	}
	if code, out, _ := run(t, "stack", "settings", "demo-stack"); code != 0 || !strings.Contains(out, "UPDATE.MODE") || !strings.Contains(out, "automatic") {
		t.Fatalf("table: %d %q", code, out)
	}
	if code, _, stderr := run(t, "stack", "settings", "broken-stack"); code == 0 || !strings.Contains(stderr, "stack check") {
		t.Fatalf("broken: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "settings", "missing-stack"); code == 0 || !strings.Contains(stderr, "no such stack") {
		t.Fatalf("missing: %d %q", code, stderr)
	}
	if code, _, _ := run(t, "stack", "settings"); code != 2 {
		t.Fatalf("no stack: %d", code)
	}
}

func TestStackSettingsSetFromFileAndStdin(t *testing.T) {
	e := startSettingsService(t)
	file := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(file, []byte("version: 1\nupdate:\n  mode: automatic\n  ceiling: patch\nbackup:\n  paths: [data/db]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := run(t, "stack", "settings", "set", "plain-stack", "--file", file, "--output", "json")
	if code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	got := objectOf(t, out)
	update, _ := settingsOfOutput(t, out)["update"].(map[string]any)
	if got["stack"] != "plain-stack" || got["changed"] != true || got["commit"] == nil || update["ceiling"] != "patch" {
		t.Fatalf("set = %v", got)
	}
	if p := e.git.Pushed(); !slices.Equal(p, []string{"qdeploy: settings of plain-stack"}) {
		t.Fatalf("pushed = %v", p)
	}
	written, err := os.ReadFile(filepath.Join(e.dir, filepath.FromSlash(servicetest.Settings("plain-stack"))))
	if err != nil || !strings.Contains(string(written), "update:\n  mode: automatic\n  ceiling: patch\n") {
		t.Fatalf("file = %q, %v", written, err)
	}
	code, out, _ = run(t, "stack", "settings", "plain-stack", "--output", "json")
	if update, _ := settingsOfOutput(t, out)["update"].(map[string]any); code != 0 || update["mode"] != "automatic" {
		t.Fatalf("show after set: %d %q", code, out)
	}

	// The same settings as JSON on standard input change nothing.
	json := `{"version":1,"update":{"mode":"automatic","ceiling":"patch"},"backup":{"paths":["data/db"],"retention":{"keep":7}}}`
	code, out, stderr = runWithStdin(t, json, "stack", "settings", "set", "plain-stack", "--file", "-", "--output", "json")
	if got := objectOf(t, out); code != 0 || got["changed"] != false || got["commit"] != nil {
		t.Fatalf("unchanged: %d %q %s", code, out, stderr)
	}
	if len(e.git.Pushed()) != 1 {
		t.Fatalf("pushed = %v", e.git.Pushed())
	}
}

func TestStackSettingsSetRefusals(t *testing.T) {
	e := startSettingsService(t)
	for _, tc := range []struct {
		name, input string
		args        []string
		code        int
		want        string
	}{
		{"no file", "", []string{"plain-stack"}, 2, "needs --file"},
		{"no stack", "", []string{"--file", "-"}, 2, "exactly one stack"},
		{"unknown member", "version: 1\nupdates: {}\n", []string{"plain-stack", "--file", "-"}, 2, "unknown field"},
		{"two documents", "version: 1\n---\nversion: 1\n", []string{"plain-stack", "--file", "-"}, 2, "one document"},
		{"empty", "", []string{"plain-stack", "--file", "-"}, 2, "empty"},
		{"bad mode", "version: 1\nupdate: {mode: sometimes}\n", []string{"plain-stack", "--file", "-"}, 2, "update.mode"},
		{"unknown variable", "version: 1\nupdate:\n  tags:\n    OTHER_VERSION: '.*'\n", []string{"plain-stack", "--file", "-"}, 2, "no service image"},
		{"no version", "update: {mode: manual}\n", []string{"plain-stack", "--file", "-"}, 2, "version"},
		{"unknown stack", "version: 1\n", []string{"missing-stack", "--file", "-"}, 1, "no such stack"},
		{"missing file", "", []string{"plain-stack", "--file", filepath.Join(t.TempDir(), "missing.yaml")}, 1, "settings file"},
	} {
		code, out, stderr := runWithStdin(t, tc.input, append([]string{"stack", "settings", "set"}, tc.args...)...)
		if code != tc.code || out != "" || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: %d %q %q", tc.name, code, out, stderr)
		}
	}
	if p := e.git.Pushed(); len(p) != 0 {
		t.Fatalf("pushed = %v", p)
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".qatlas", "qdeploy", "plain-stack.yaml")); !os.IsNotExist(err) {
		t.Fatalf("settings were written: %v", err)
	}
}
