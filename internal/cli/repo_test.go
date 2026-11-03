package cli

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/hostkey/hostkeytest"
	"github.com/castrowithcee/qatlas-deploy/internal/service/servicetest"
)

type repoEnv struct {
	state string
	git   *servicetest.Git
	srv   *hostkeytest.Server
	dir   string
}

func startRepoService(t *testing.T) *repoEnv {
	t.Helper()
	g := servicetest.New()
	g.Files = servicetest.Stacks()
	old := testGit
	testGit = g
	t.Cleanup(func() { testGit = old })
	startService(t, func(net.Conn) error { return nil })
	e := &repoEnv{state: stateDir(), git: g, srv: hostkeytest.Start(t), dir: filepath.Join(t.TempDir(), "stacks")}
	if code, _, stderr := run(t, "key", "generate"); code != 0 {
		t.Fatalf("key generate: %d %s", code, stderr)
	}
	return e
}

func (e *repoEnv) knownHosts() string {
	data, _ := os.ReadFile(filepath.Join(e.state, "known_hosts"))
	return string(data)
}

func useTerminalStdin(t *testing.T, answer string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(answer); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	useStdin(t, r)
	oldTerm := isTerminal
	isTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { isTerminal = oldTerm; _ = r.Close() })
}

func TestRepoSetWithoutTerminalNeedsTheFingerprint(t *testing.T) {
	e := startRepoService(t)
	useStdin(t, strings.NewReader(""))
	code, stdout, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir)
	if code == 0 || stdout != "" || !strings.Contains(stderr, e.srv.Key().Fingerprint) || !strings.Contains(stderr, "--fingerprint") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if e.knownHosts() != "" || e.git.Clones() != 0 {
		t.Fatal("a refused set pinned a key or cloned")
	}
}

func TestRepoSetWithFingerprintBindsAndStacksLists(t *testing.T) {
	e := startRepoService(t)
	code, stdout, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint, "--output", "json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout, err)
	}
	if got["dir"] != e.dir || got["cloned"] != true || got["host_key_fingerprint"] != e.srv.Key().Fingerprint {
		t.Fatalf("result = %v", got)
	}
	if !strings.HasPrefix(e.knownHosts(), e.srv.Pattern()+" ssh-ed25519 ") {
		t.Fatalf("known_hosts = %q", e.knownHosts())
	}

	code, stdout, stderr = run(t, "stacks", "--output", "json")
	if code != 0 {
		t.Fatalf("stacks: code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"demo-stack", "automatic", "DEMO_APP_VERSION=1.2.3", "plain-stack", "none", "broken-stack", "qdeploy.yaml"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stacks output lacks %q:\n%s", want, stdout)
		}
	}
	if code, stdout, _ := run(t, "stacks"); code != 0 || !strings.Contains(stdout, "demo-stack") {
		t.Fatalf("stacks table: code %d, %q", code, stdout)
	}

	code, stdout, stderr = run(t, "repo", "fetch", "--output", "json")
	if code != 0 || !strings.Contains(stdout, `"updates_available":false`) {
		t.Fatalf("fetch: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestRepoSetRefusesAWrongFingerprint(t *testing.T) {
	e := startRepoService(t)
	other := hostkeytest.Start(t).Key().Fingerprint
	code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", other)
	if code == 0 || !strings.Contains(stderr, "nothing was stored") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if e.knownHosts() != "" || e.git.Clones() != 0 {
		t.Fatal("a refused set pinned a key or cloned")
	}
	if code, _, _ := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", "abc"); code != 2 {
		t.Fatalf("malformed fingerprint: code %d, want 2", code)
	}
}

func TestRepoSetAsksAtATerminal(t *testing.T) {
	e := startRepoService(t)
	useTerminalStdin(t, "n\n")
	code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir)
	if code == 0 || !strings.Contains(stderr, e.srv.Key().Fingerprint) || !strings.Contains(stderr, "not confirmed") {
		t.Fatalf("declined: code %d, stderr %q", code, stderr)
	}
	if e.knownHosts() != "" || e.git.Clones() != 0 {
		t.Fatal("a declined set pinned a key or cloned")
	}

	useTerminalStdin(t, "yes\n")
	code, _, stderr = run(t, "repo", "set", e.srv.URL(), "--dir", e.dir)
	if code != 0 || e.knownHosts() == "" || e.git.Clones() != 1 {
		t.Fatalf("confirmed: code %d, stderr %q, clones %d", code, stderr, e.git.Clones())
	}

	// Pinned already: no question, no flag.
	useStdin(t, strings.NewReader(""))
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir); code != 0 {
		t.Fatalf("repeat: code %d, stderr %q", code, stderr)
	}
}

func TestRepoSetAbortsOnAChangedHostKey(t *testing.T) {
	e := startRepoService(t)
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint); code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	pinned := e.knownHosts()
	e.srv.Rekey(t)
	code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint)
	if code == 0 || !strings.Contains(stderr, "differs from the pinned key") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if e.knownHosts() != pinned {
		t.Fatal("the pinned key was replaced")
	}
}

func TestRepoSetRejectsBadInput(t *testing.T) {
	startRepoService(t)
	for _, args := range [][]string{
		{"repo", "set"},
		{"repo", "set", "https://git.example.test/stacks.git"},
		{"repo", "set", "git.example.test:stacks.git", "--dir", "relative/stacks"},
		{"repo", "set", "git.example.test:stacks.git", "--dir", "/srv/../stacks"},
	} {
		if code, _, _ := run(t, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}

func TestStacksAndFetchWithoutBindingNameTheNextStep(t *testing.T) {
	startRepoService(t)
	for _, cmd := range []string{"stacks", "fetch"} {
		args := []string{cmd}
		if cmd == "fetch" {
			args = []string{"repo", "fetch"}
		}
		code, _, stderr := run(t, args...)
		if code == 0 || !strings.Contains(stderr, "repo set") {
			t.Errorf("%s: code %d, stderr %q", cmd, code, stderr)
		}
	}
}

func TestRepoFetchWithoutRunningService(t *testing.T) {
	useEndpoint(t, channeltest.Endpoint(t))
	if code, _, _ := run(t, "repo", "fetch"); code != ExitServiceNotRunning {
		t.Fatalf("code %d, want %d", code, ExitServiceNotRunning)
	}
}

func bindWithFiles(t *testing.T, files map[string]string, tracked ...string) *repoEnv {
	t.Helper()
	e := startRepoService(t)
	e.git.Files = files
	e.git.Tracked = tracked
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint); code != 0 {
		t.Fatalf("repo set: %d %s", code, stderr)
	}
	return e
}

const cliSecret = "not-a-real-secret-4711"

func TestCheckCleanStackExitsZero(t *testing.T) {
	bindWithFiles(t, map[string]string{
		"demo-stack/compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_APP_VERSION}\n",
		"demo-stack/.env":         "DEMO_APP_VERSION=1.2.3\n",
		"demo-stack/secrets.env":  "DEMO_PASSWORD=" + cliSecret + "\n",
	})
	for _, args := range [][]string{{"check"}, {"check", "demo-stack"}, {"check", "--output", "json"}} {
		code, stdout, stderr := run(t, args...)
		if code != 0 || stderr != "" || strings.Contains(stdout, cliSecret) || strings.Contains(stdout, "error") {
			t.Fatalf("%v: code %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestCheckErrorsExitNonZeroInTextAndJSON(t *testing.T) {
	bindWithFiles(t, map[string]string{
		"demo-stack/compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:latest\n",
		"demo-stack/.env":         "DEMO_PASSWORD=x\n",
		"demo-stack/secrets.env":  "DEMO_PASSWORD=" + cliSecret + "\n",
	}, "demo-stack/secrets.env")

	code, stdout, stderr := run(t, "check")
	if code != 1 || !strings.Contains(stderr, "error(s)") {
		t.Fatalf("text: code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"demo-stack", "floating-image-tag", "duplicate-key", "tracked-secrets-env", "error"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("text output lacks %q:\n%s", want, stdout)
		}
	}

	code, stdout, _ = run(t, "check", "demo-stack", "--output", "json")
	if code != 1 {
		t.Fatalf("json: code %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		var wrapped struct{ Rows []map[string]any }
		if err2 := json.Unmarshal([]byte(stdout), &wrapped); err2 != nil {
			t.Fatalf("invalid JSON %q: %v", stdout, err)
		}
		rows = wrapped.Rows
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %v", rows)
	}
	for _, r := range rows {
		for _, k := range []string{"stack", "rule", "severity", "cause", "fix"} {
			if s, _ := r[k].(string); s == "" {
				t.Errorf("row lacks %s: %v", k, r)
			}
		}
	}
	for _, args := range [][]string{{"check"}, {"check", "--output", "json"}} {
		_, out, errOut := run(t, args...)
		if strings.Contains(out+errOut, cliSecret) {
			t.Fatalf("%v: secret value in output", args)
		}
	}
}

func TestCheckWarningsExitZero(t *testing.T) {
	e := bindWithFiles(t, map[string]string{
		"demo-stack/compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_APP_VERSION}\n",
		"demo-stack/.env":         "DEMO_APP_VERSION=1.2.3\n",
		"demo-stack/secrets.env":  "DEMO_PASSWORD=" + cliSecret + "\n",
	})
	e.git.NotIgnored = true
	code, stdout, _ := run(t, "check")
	if code != 0 || !strings.Contains(stdout, "secrets-env-not-ignored") || !strings.Contains(stdout, "warning") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
}

func TestCheckUsageAndUnknownStack(t *testing.T) {
	bindWithFiles(t, map[string]string{"demo-stack/compose.yaml": "services: {}\n"})
	if code, _, _ := run(t, "check", "a", "b"); code != 2 {
		t.Fatalf("two arguments: code %d, want 2", code)
	}
	if code, _, stderr := run(t, "check", "nope"); code == 0 || !strings.Contains(stderr, "no such stack") {
		t.Fatalf("unknown stack: code %d, stderr %q", code, stderr)
	}
}
