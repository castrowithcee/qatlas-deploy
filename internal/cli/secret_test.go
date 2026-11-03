package cli

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/hostkey/hostkeytest"
	"github.com/castrowithcee/qatlas-deploy/internal/service/servicetest"
)

const (
	fakeSecretValue  = "demo-value-not-real-1"
	otherSecretValue = "demo-other-not-real-2"
)

func startSecretService(t *testing.T, log *syncBuffer) *repoEnv {
	t.Helper()
	g := servicetest.New()
	g.Files = servicetest.Stacks()
	old := testGit
	testGit = g
	t.Cleanup(func() { testGit = old })
	startServiceLogged(t, func(net.Conn) error { return nil }, log)
	e := &repoEnv{state: stateDir(), git: g, srv: hostkeytest.Start(t), dir: filepath.Join(t.TempDir(), "stacks")}
	if code, _, stderr := run(t, "key", "generate"); code != 0 {
		t.Fatalf("key generate: %d %s", code, stderr)
	}
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint); code != 0 {
		t.Fatalf("repo set: %d %s", code, stderr)
	}
	return e
}

func TestSecretRoundTripNeverShowsTheValue(t *testing.T) {
	var log syncBuffer
	e := startSecretService(t, &log)
	var all []string

	code, stdout, stderr := runWithStdin(t, fakeSecretValue+"\n", "stack", "secret", "set", "plain-stack", "DB_PASSWORD")
	all = append(all, stdout, stderr)
	if code != 0 || !strings.Contains(stdout, "DB_PASSWORD") {
		t.Fatalf("set: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	code, stdout, stderr = runWithStdin(t, otherSecretValue, "stack", "secret", "set", "plain-stack", "API_KEY", "--output", "json")
	all = append(all, stdout, stderr)
	var set map[string]any
	if err := json.Unmarshal([]byte(stdout), &set); code != 0 || err != nil || set["key"] != "API_KEY" || set["replaced"] != false {
		t.Fatalf("set json: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	info, err := os.Stat(filepath.Join(e.dir, "plain-stack", "secrets.env"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("info %v, err %v", info, err)
	}

	code, stdout, stderr = run(t, "stack", "secret", "list", "plain-stack")
	all = append(all, stdout, stderr)
	if code != 0 || !strings.Contains(stdout, "API_KEY") || !strings.Contains(stdout, "DB_PASSWORD") {
		t.Fatalf("list: code %d, stdout %q", code, stdout)
	}
	code, stdout, stderr = run(t, "stack", "secret", "list", "plain-stack", "--output", "json")
	all = append(all, stdout, stderr)
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); code != 0 || err != nil || len(rows) != 2 {
		t.Fatalf("list json: code %d, stdout %q", code, stdout)
	}

	// A key of .env is refused with a usage error, and a missing key on delete is not-found.
	code, stdout, stderr = runWithStdin(t, fakeSecretValue, "stack", "secret", "set", "plain-stack", "DEMO_APP_VERSION")
	all = append(all, stdout, stderr)
	if code != 2 || !strings.Contains(stderr, ".env") {
		t.Fatalf("set of an .env key: code %d, stderr %q", code, stderr)
	}
	code, stdout, stderr = run(t, "stack", "secret", "delete", "plain-stack", "NO_SUCH")
	all = append(all, stdout, stderr)
	if code == 0 || !strings.Contains(stderr, "not-found") {
		t.Fatalf("delete missing: code %d, stderr %q", code, stderr)
	}
	code, stdout, stderr = run(t, "stack", "secret", "delete", "plain-stack", "DB_PASSWORD")
	all = append(all, stdout, stderr)
	if code != 0 {
		t.Fatalf("delete: code %d, stderr %q", code, stderr)
	}
	all = append(all, log.String())
	for _, text := range all {
		if strings.Contains(text, fakeSecretValue) || strings.Contains(text, otherSecretValue) {
			t.Fatalf("a value appears in output or log: %q", text)
		}
	}
	stored, err := os.ReadFile(filepath.Join(e.dir, "plain-stack", "secrets.env"))
	if err != nil || strings.Contains(string(stored), "DB_PASSWORD") || !strings.Contains(string(stored), otherSecretValue) {
		t.Fatalf("file = %q, %v", stored, err)
	}
}

func TestSecretSetRefusesValueAsArgumentAndBadKey(t *testing.T) {
	useStdin(t, strings.NewReader(fakeSecretValue))
	code, stdout, stderr := run(t, "stack", "secret", "set", "plain-stack", "DB_PASSWORD", fakeSecretValue)
	if code != 2 || stdout != "" || strings.Contains(stderr, fakeSecretValue) {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	code, _, stderr = runWithStdin(t, fakeSecretValue, "stack", "secret", "set", "plain-stack", "bad key")
	if code != 2 || strings.Contains(stderr, fakeSecretValue) {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	code, _, stderr = runWithStdin(t, "", "stack", "secret", "set", "plain-stack", "OK_KEY")
	if code != 2 {
		t.Fatalf("empty stdin: code %d, stderr %q", code, stderr)
	}
}
