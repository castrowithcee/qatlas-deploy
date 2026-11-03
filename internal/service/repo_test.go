package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/hostkey/hostkeytest"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
	"github.com/castrowithcee/qatlas-deploy/internal/service/servicetest"
)

type harness struct {
	t     *testing.T
	state string
	git   *servicetest.Git
	srv   *hostkeytest.Server
	url   string
	dir   string
	call  func(command string, args, result any) error
}

func start(t *testing.T) *harness { return startWith(t, nil) }

func startWith(t *testing.T, dep engine.Deployer) *harness {
	t.Helper()
	h := &harness{t: t, state: filepath.Join(t.TempDir(), "state"), git: servicetest.New(), srv: hostkeytest.Start(t)}
	h.git.Files = servicetest.Stacks()
	h.url = h.srv.URL()
	h.dir = filepath.Join(t.TempDir(), "stacks")
	ep := channeltest.Endpoint(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), service.Config{
			Endpoint: ep, StateDir: h.state, PeerCheck: func(net.Conn) error { return nil }, Git: h.git, Deployer: dep,
		})
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
	h.call = func(command string, args, result any) error {
		cl, err := client.Dial(context.Background(), ep)
		if err != nil {
			t.Fatal(err)
		}
		defer cl.Close()
		return cl.Call(context.Background(), command, args, result)
	}
	return h
}

func (h *harness) withKey() {
	h.t.Helper()
	if err := h.call(control.KeyGenerateCommand, control.KeyGenerateArgs{}, nil); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) set(fingerprint string) (control.RepoSetResult, error) {
	var res control.RepoSetResult
	err := h.call(control.RepoSetCommand, control.RepoSetArgs{URL: h.url, Dir: h.dir, Fingerprint: fingerprint}, &res)
	return res, err
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var remote *control.Error
	if !errors.As(err, &remote) || remote.Code != code {
		t.Fatalf("got %v, want code %s", err, code)
	}
}

func (h *harness) knownHosts() string {
	data, _ := os.ReadFile(filepath.Join(h.state, "known_hosts"))
	return string(data)
}

func TestProbeShowsTheKeyAndPinsNothing(t *testing.T) {
	h := start(t)
	var res control.RepoProbeResult
	if err := h.call(control.RepoProbeCommand, control.RepoProbeArgs{URL: h.url}, &res); err != nil {
		t.Fatal(err)
	}
	want := h.srv.Key()
	if res.Fingerprint != want.Fingerprint || res.PublicKey != want.Authorized || res.KeyType != "ssh-ed25519" ||
		res.Host != h.srv.Host || res.Port != h.srv.Port || res.Pinned {
		t.Fatalf("probe = %+v, want key %+v", res, want)
	}
	if h.knownHosts() != "" {
		t.Fatal("probing pinned a key")
	}
}

func TestProbeRefusesAnInvalidURL(t *testing.T) {
	h := start(t)
	for _, u := range []string{"", "https://git.example.test/x.git", "-oProxyCommand=id", "/srv/x.git"} {
		wantCode(t, h.call(control.RepoProbeCommand, control.RepoProbeArgs{URL: u}, nil), control.CodeInvalid)
	}
}

func TestSetPinsTheConfirmedKeyAndClones(t *testing.T) {
	h := start(t)
	h.withKey()
	res, err := h.set(h.srv.Key().Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Cloned || res.Dir != h.dir || res.Fingerprint != h.srv.Key().Fingerprint || res.Head != h.git.Head {
		t.Fatalf("result = %+v", res)
	}
	if want := h.srv.Pattern() + " " + h.srv.Key().Authorized + "\n"; h.knownHosts() != want {
		t.Fatalf("known_hosts = %q, want %q", h.knownHosts(), want)
	}
	if !strings.Contains(h.srv.Pattern(), "[127.0.0.1]:") {
		t.Fatalf("pattern %q lacks the port form", h.srv.Pattern())
	}
	cfg := h.git.LastConfig()
	if cfg.KnownHosts != filepath.Join(h.state, "known_hosts") || cfg.KeyFile != filepath.Join(h.state, "deploy-key") {
		t.Fatalf("git configuration = %+v", cfg)
	}
	var probe control.RepoProbeResult
	if err := h.call(control.RepoProbeCommand, control.RepoProbeArgs{URL: h.url}, &probe); err != nil || !probe.Pinned {
		t.Fatalf("probe after set: %+v, %v", probe, err)
	}
	// The same binding again is fine and clones nothing.
	again, err := h.set(h.srv.Key().Fingerprint)
	if err != nil || again.Cloned || h.git.Clones() != 1 {
		t.Fatalf("second set: %+v, %v, clones %d", again, err, h.git.Clones())
	}
}

func TestSetRefusesAWrongFingerprint(t *testing.T) {
	h := start(t)
	h.withKey()
	other := hostkeytest.NewSigner(t)
	wrong := ssh.FingerprintSHA256(other.PublicKey())
	wantCode(t, errOf(h.set(wrong)), control.CodeHostKey)
	if h.knownHosts() != "" || h.git.Clones() != 0 {
		t.Fatal("a refused set pinned a key or cloned")
	}
	if _, err := os.Lstat(filepath.Join(h.state, "stacks-repo.json")); err == nil {
		t.Fatal("a refused set stored a configuration")
	}
	wantCode(t, errOf(h.set("")), control.CodeInvalid)
}

func TestSetRefusesAChangedHostKey(t *testing.T) {
	h := start(t)
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	pinned := h.knownHosts()
	h.srv.Rekey(t)
	wantCode(t, h.call(control.RepoProbeCommand, control.RepoProbeArgs{URL: h.url}, nil), control.CodeHostKey)
	// Even the fingerprint of the new key does not replace the pinned one.
	wantCode(t, errOf(h.set(h.srv.Key().Fingerprint)), control.CodeHostKey)
	if h.knownHosts() != pinned {
		t.Fatal("the pinned key was changed")
	}
}

func TestSetNeedsADeployKey(t *testing.T) {
	h := start(t)
	wantCode(t, errOf(h.set(h.srv.Key().Fingerprint)), control.CodeNotFound)
	if h.knownHosts() != "" {
		t.Fatal("a key was pinned without a deploy key")
	}
}

func TestSetRefusesBadDirectories(t *testing.T) {
	h := start(t)
	h.withKey()
	fp := h.srv.Key().Fingerprint
	for _, dir := range []string{"relative/stacks", "/", "/srv/../etc", "/srv/stacks/", "/srv/sta cks", h.state, h.state + "/sub", filepath.Dir(h.state)} {
		h.dir = dir
		wantCode(t, errOf(h.set(fp)), control.CodeInvalid)
	}
	if h.knownHosts() != "" || h.git.Clones() != 0 {
		t.Fatal("a refused directory had an effect")
	}
}

func TestSetRefusesANonEmptyForeignDirectory(t *testing.T) {
	h := start(t)
	h.withKey()
	fp := h.srv.Key().Fingerprint
	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantCode(t, errOf(h.set(fp)), control.CodeInvalid)
	h.git.SetOrigin(h.dir, "ssh://other.example.test/stacks.git")
	wantCode(t, errOf(h.set(fp)), control.CodeInvalid)
	h.git.SetOrigin(h.dir, h.url)
	res, err := h.set(fp)
	if err != nil || res.Cloned || h.git.Clones() != 0 {
		t.Fatalf("an existing clone of the URL: %+v, %v", res, err)
	}
}

func TestSetRefusesAnotherBinding(t *testing.T) {
	h := start(t)
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	h.dir += "-other"
	wantCode(t, errOf(h.set(h.srv.Key().Fingerprint)), control.CodeRepoBound)
}

func TestFetchAndStacksNeedABinding(t *testing.T) {
	h := start(t)
	wantCode(t, h.call(control.RepoFetchCommand, nil, nil), control.CodeNoRepo)
	wantCode(t, h.call(control.StacksListCommand, nil, nil), control.CodeNoRepo)
}

func TestFetchReportsUpdates(t *testing.T) {
	h := start(t)
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	var res control.RepoFetchResult
	if err := h.call(control.RepoFetchCommand, nil, &res); err != nil || res.UpdatesAvailable || res.Head != h.git.Head {
		t.Fatalf("fetch: %+v, %v", res, err)
	}
	h.git.SetRemote("89abcdef0123456789abcdef0123456789abcdef")
	if err := h.call(control.RepoFetchCommand, nil, &res); err != nil || !res.UpdatesAvailable || res.RemoteHead != h.git.Remote {
		t.Fatalf("fetch with news: %+v, %v", res, err)
	}
}

func TestFetchAbortsOnAHostKeyFailure(t *testing.T) {
	h := start(t)
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	h.git.SetFetchErr(errors.New("fetch failed: Host key verification failed. fatal: Could not read from remote repository."))
	wantCode(t, h.call(control.RepoFetchCommand, nil, nil), control.CodeHostKey)
	h.git.SetFetchErr(errors.New("fetch failed: connection timed out"))
	wantCode(t, h.call(control.RepoFetchCommand, nil, nil), control.CodeRemote)
}

func TestCloneFailureLeavesNoBinding(t *testing.T) {
	h := start(t)
	h.withKey()
	h.git.CloneErr = errors.New("clone failed: permission denied")
	wantCode(t, errOf(h.set(h.srv.Key().Fingerprint)), control.CodeRemote)
	wantCode(t, h.call(control.StacksListCommand, nil, nil), control.CodeNoRepo)
}

func TestStacksListsValidInvalidAndPlainStacks(t *testing.T) {
	h := start(t)
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	var res control.StacksResult
	if err := h.call(control.StacksListCommand, nil, &res); err != nil {
		t.Fatal(err)
	}
	if res.Dir != h.dir || len(res.Stacks) != 3 {
		t.Fatalf("result = %+v", res)
	}
	by := map[string]control.StackInfo{}
	for _, s := range res.Stacks {
		by[s.Name] = s
	}
	demo := by["demo-stack"]
	if demo.Update != "automatic" || demo.Error != "" || len(demo.Images) != 1 ||
		demo.Images[0].Var != "DEMO_APP_VERSION" || demo.Images[0].Version != "1.2.3" {
		t.Fatalf("demo-stack = %+v", demo)
	}
	if plain := by["plain-stack"]; plain.Update != "none" || plain.Error != "" || plain.Images[0].Version != "0.9.1" {
		t.Fatalf("plain-stack = %+v", plain)
	}
	if broken := by["broken-stack"]; broken.Error == "" || !strings.Contains(broken.Error, "qdeploy.yaml") {
		t.Fatalf("broken-stack = %+v", broken)
	}
}

func TestRepoCommandsRejectUnknownArguments(t *testing.T) {
	h := start(t)
	wantCode(t, h.call(control.RepoSetCommand, map[string]string{"url": h.url, "extra": "x"}, nil), control.CodeBadRequest)
	wantCode(t, h.call(control.RepoFetchCommand, map[string]string{"extra": "x"}, nil), control.CodeBadRequest)
}

func errOf(_ control.RepoSetResult, err error) error { return err }

// The engine runs from the start of the service and picks up a binding made later without a restart.
func TestEngineStartsPollingAfterRepoSet(t *testing.T) {
	h := start(t)
	h.withKey()
	statePath := filepath.Join(h.state, "deploy", "state.json")
	time.Sleep(5 * time.Millisecond)
	if _, err := os.Stat(statePath); err == nil {
		t.Fatal("engine state written without a binding")
	}
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		data, err := os.ReadFile(statePath)
		if err == nil && strings.Contains(string(data), `"demo-stack"`) {
			if strings.Contains(string(data), `"deployed"`) {
				t.Fatalf("a service without a deployer recorded a deployment: %s", data)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("engine did not poll after repo set: %v %s", err, data)
		}
	}
}

const checkSecret = "not-a-real-secret-4711"

func conformantFiles() map[string]string {
	return map[string]string{
		"good-stack/compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_APP_VERSION}\n",
		"good-stack/.env":         "DEMO_APP_VERSION=1.2.3\n",
		"good-stack/secrets.env":  "DEMO_PASSWORD=" + checkSecret + "\n",
	}
}

func TestCheckReportsFindingsPerStack(t *testing.T) {
	h := start(t)
	h.withKey()
	h.git.Files = conformantFiles()
	h.git.Files["bad-stack/compose.yaml"] = "services:\n  app:\n    image: registry.example.test/demo/app:latest\n"
	h.git.Files["bad-stack/.env"] = "DEMO_PASSWORD=x\n"
	h.git.Files["bad-stack/secrets.env"] = "DEMO_PASSWORD=" + checkSecret + "\n"
	h.git.Tracked = []string{"bad-stack/secrets.env"}
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}

	var res control.StacksCheckResult
	if err := h.call(control.StacksCheckCommand, control.StacksCheckArgs{}, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Stacks) != 2 || res.Dir != h.dir {
		t.Fatalf("result = %+v", res)
	}
	rules := map[string]string{}
	for _, f := range res.Findings {
		if f.Stack == "good-stack" {
			t.Errorf("finding for the conformant stack: %+v", f)
		}
		if f.Cause == "" || f.Fix == "" || (f.Severity != "error" && f.Severity != "warning") {
			t.Errorf("incomplete finding %+v", f)
		}
		if strings.Contains(f.Cause+f.Fix, checkSecret) {
			t.Errorf("secret value in %+v", f)
		}
		rules[f.Rule] = f.Severity
	}
	for _, want := range []string{"floating-image-tag", "duplicate-key", "tracked-secrets-env"} {
		if rules[want] != "error" {
			t.Errorf("missing error %s in %v", want, rules)
		}
	}
	if res.Errors != 3 || res.Warnings != 0 {
		t.Fatalf("counts = %d errors, %d warnings", res.Errors, res.Warnings)
	}

	var one control.StacksCheckResult
	if err := h.call(control.StacksCheckCommand, control.StacksCheckArgs{Stack: "good-stack"}, &one); err != nil {
		t.Fatal(err)
	}
	if len(one.Stacks) != 1 || one.Stacks[0] != "good-stack" || len(one.Findings) != 0 || one.Findings == nil {
		t.Fatalf("single stack result = %+v", one)
	}
}

func TestCheckRefusesUnknownStackAndMissingBinding(t *testing.T) {
	h := start(t)
	wantCode(t, h.call(control.StacksCheckCommand, control.StacksCheckArgs{}, nil), control.CodeNoRepo)
	h.withKey()
	h.git.Files = conformantFiles()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	wantCode(t, h.call(control.StacksCheckCommand, control.StacksCheckArgs{Stack: "nope"}, nil), control.CodeNotFound)
	wantCode(t, h.call(control.StacksCheckCommand, map[string]string{"bogus": "x"}, nil), control.CodeBadRequest)
}

func TestCheckReducesRepoTextToPrintable(t *testing.T) {
	h := start(t)
	h.withKey()
	h.git.Files = map[string]string{
		"odd-stack/compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:1.0.0\n",
		"odd-stack/.env":         "COMPOSE_FILE=compose.yaml:a\x1b[31mb.yaml\n",
	}
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	var res control.StacksCheckResult
	if err := h.call(control.StacksCheckCommand, control.StacksCheckArgs{}, &res); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range res.Findings {
		found = found || f.Rule == "missing-compose-file"
		if strings.ContainsRune(f.Cause+f.Fix+f.Stack, '\x1b') {
			t.Errorf("control character in %+v", f)
		}
	}
	if !found {
		t.Fatalf("findings = %+v", res.Findings)
	}
}
