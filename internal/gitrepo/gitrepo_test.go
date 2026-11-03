package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fixtureGit runs git for test setup with its own clean environment.
func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Demo", "GIT_AUTHOR_EMAIL=demo@example.test",
		"GIT_COMMITTER_NAME=Demo", "GIT_COMMITTER_EMAIL=demo@example.test",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// remoteFixture is a bare repository plus a work tree that pushes to it.
type remoteFixture struct {
	t          *testing.T
	bare, work string
}

func newRemote(t *testing.T) *remoteFixture {
	t.Helper()
	if _, err := lookIn(fixedPath, "git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	f := &remoteFixture{t: t, bare: filepath.Join(root, "stacks.git"), work: filepath.Join(root, "work")}
	for _, d := range []string{f.bare, f.work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fixtureGit(t, f.bare, "init", "--bare", "-q", "-b", "main")
	fixtureGit(t, f.work, "init", "-q", "-b", "main")
	f.commit(map[string]string{"README.md": "x\n", "web/compose.yaml": "a\n", "db/compose.yaml": "a\n"})
	return f
}

func (f *remoteFixture) commit(files map[string]string) string {
	writeTree(f.t, f.work, files)
	fixtureGit(f.t, f.work, "add", "-A")
	fixtureGit(f.t, f.work, "commit", "-q", "-m", "change")
	fixtureGit(f.t, f.work, "push", "-q", f.bare, "main")
	return fixtureGit(f.t, f.work, "rev-parse", "HEAD")
}

func testConfig(t *testing.T) Config {
	d := t.TempDir()
	return Config{KeyFile: d + "/deploy-key", KnownHosts: d + "/known_hosts", test: testOptions{allowLocal: true}}
}

func cloneFixture(t *testing.T, f *remoteFixture) (*Repo, Config) {
	t.Helper()
	cfg := testConfig(t)
	r, err := Clone(context.Background(), cfg, f.bare, filepath.Join(t.TempDir(), "copy"))
	if err != nil {
		t.Fatal(err)
	}
	return r, cfg
}

func TestCloneFetchStatusChangedFolders(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	r, _ := cloneFixture(t, f)

	if _, err := os.Stat(filepath.Join(r.dir, "web", "compose.yaml")); err != nil {
		t.Fatalf("working copy not checked out: %v", err)
	}
	head, err := r.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changes, err := r.Status(ctx); err != nil || len(changes) != 0 {
		t.Fatalf("clean copy: changes=%v err=%v", changes, err)
	}

	next := f.commit(map[string]string{"web/compose.yaml": "b\n", "web/sub/x.env": "1\n", "top.txt": "y\n"})
	if rh, _ := r.RemoteHead(ctx); rh != head {
		t.Fatalf("remote head moved before fetch: %s", rh)
	}
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if rh, err := r.RemoteHead(ctx); err != nil || rh != next {
		t.Fatalf("RemoteHead = %q, %v; want %s", rh, err, next)
	}
	if h, _ := r.Head(ctx); h != head {
		t.Fatal("fetch moved HEAD")
	}
	if _, err := os.Stat(filepath.Join(r.dir, "top.txt")); err == nil {
		t.Fatal("fetch updated the working copy")
	}
	folders, err := r.ChangedFolders(ctx, head, next)
	if err != nil || !slices.Equal(folders, []string{"web"}) {
		t.Fatalf("ChangedFolders = %v, %v", folders, err)
	}
	if folders, _ := r.ChangedFolders(ctx, head, head); len(folders) != 0 {
		t.Fatalf("same commit: %v", folders)
	}
	if _, err := r.ChangedFolders(ctx, "--output=x", next); err == nil {
		t.Fatal("accepted a non-commit-id revision")
	}

	writeTree(t, r.dir, map[string]string{"db/compose.yaml": "local\n", "new dir/file with space.txt": "u\n"})
	changes, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{{" M", "db/compose.yaml"}, {"??", "new dir/file with space.txt"}}
	if !slices.Equal(changes, want) {
		t.Fatalf("Status = %v; want %v", changes, want)
	}
}

func TestCloneRefusesNonEmptyTarget(t *testing.T) {
	f := newRemote(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"keep.txt": "x"})
	if _, err := Clone(context.Background(), testConfig(t), f.bare, dir); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("err = %v; want ErrNotEmpty", err)
	}
	// an existing empty directory is fine
	if _, err := Clone(context.Background(), testConfig(t), f.bare, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestProductionRefusesLocalRemote(t *testing.T) {
	f := newRemote(t)
	cfg := testConfig(t)
	cfg.test = testOptions{}
	for _, u := range []string{f.bare, "file://" + f.bare} {
		if _, err := Clone(context.Background(), cfg, u, filepath.Join(t.TempDir(), "c")); !errors.Is(err, ErrInvalidURL) {
			t.Fatalf("Clone(%q) err = %v; want ErrInvalidURL", u, err)
		}
	}
}

// Even if a local URL reached git, production must not allow the file protocol.
func TestProductionGitRefusesFileProtocol(t *testing.T) {
	f := newRemote(t)
	cfg := testConfig(t)
	cfg.test = testOptions{}
	r, err := Open(cfg, filepath.Join(t.TempDir(), "c"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.run(context.Background(), false, maxStdout, "ls-remote", "--", f.bare)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v; want a protocol refusal", err)
	}
}

func TestEnvironmentIsFresh(t *testing.T) {
	cfg := testConfig(t)
	r, err := Open(cfg, filepath.Join(t.TempDir(), "c"))
	if err != nil {
		t.Fatal(err)
	}
	foreign := map[string]string{
		"GIT_DIR": "/nonexistent", "GIT_WORK_TREE": "/nonexistent", "GIT_CONFIG_PARAMETERS": "'alias.x'='!id'",
		"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.pager", "GIT_CONFIG_VALUE_0": "id",
		"GIT_SSH_COMMAND": "evil", "GIT_SSH": "evil", "GIT_PROXY_COMMAND": "evil", "GIT_EXEC_PATH": "/nonexistent",
		"GIT_ASKPASS": "evil", "SSH_ASKPASS": "evil", "SSH_AUTH_SOCK": "/tmp/evil.sock", "HOME": "/nonexistent",
		"XDG_CONFIG_HOME": "/nonexistent", "GIT_TEMPLATE_DIR": "/nonexistent", "LD_PRELOAD": "/nonexistent.so",
		"GIT_ALLOW_PROTOCOL": "ext:file", "SECRET_TOKEN": "demo-secret-value",
	}
	for k, v := range foreign {
		t.Setenv(k, v)
	}
	tmp := t.TempDir()
	env := r.environment(tmp)
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	wantKeys := []string{"PATH", "HOME", "XDG_CONFIG_HOME", "TMPDIR", "LC_ALL", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL",
		"GIT_TERMINAL_PROMPT", "GIT_OPTIONAL_LOCKS", "GIT_ALLOW_PROTOCOL", "GIT_CEILING_DIRECTORIES", "SSH_AUTH_SOCK", "GIT_SSH_COMMAND"}
	if len(got) != len(wantKeys) || len(env) != len(wantKeys) {
		t.Fatalf("environment = %v", env)
	}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	for k, v := range foreign {
		if got[k] == v {
			t.Errorf("%s was inherited", k)
		}
	}
	if got["HOME"] != filepath.Join(tmp, "home") || got["SSH_AUTH_SOCK"] != "" || got["GIT_TERMINAL_PROMPT"] != "0" ||
		got["GIT_CONFIG_GLOBAL"] != "/dev/null" || got["GIT_CONFIG_NOSYSTEM"] != "1" {
		t.Errorf("unexpected values: %v", got)
	}
	ssh := got["GIT_SSH_COMMAND"]
	for _, w := range []string{"ssh -F /dev/null", "BatchMode=yes", "IdentitiesOnly=yes", "IdentityFile=" + cfg.KeyFile,
		"StrictHostKeyChecking=yes", "UserKnownHostsFile=" + cfg.KnownHosts, "GlobalKnownHostsFile=/dev/null"} {
		if !strings.Contains(ssh, w) {
			t.Errorf("GIT_SSH_COMMAND lacks %q: %s", w, ssh)
		}
	}
	if strings.ContainsAny(ssh, `'"\$;&|`+"`") {
		t.Errorf("GIT_SSH_COMMAND needs shell quoting: %s", ssh)
	}
	prod := *r
	prod.cfg.test = testOptions{}
	if p := envValue(prod.environment(tmp), "GIT_ALLOW_PROTOCOL"); p != "ssh" {
		t.Errorf("production GIT_ALLOW_PROTOCOL = %q", p)
	}
}

func envValue(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

// With hostile variables and files in the caller's world, a real run must still behave: an inherited GIT_DIR
// would break it, an inherited alias, pager or hook path would leave a marker.
func TestRunIgnoresCallerWorld(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	r, _ := cloneFixture(t, f)
	marker := filepath.Join(t.TempDir(), "marker")
	script := filepath.Join(t.TempDir(), "evil.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[core]\n\tfsmonitor = "+script+"\n\thooksPath = "+filepath.Dir(script)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.fsmonitor")
	t.Setenv("GIT_CONFIG_VALUE_0", script)
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.fsmonitor'='"+script+"'")
	t.Setenv("GIT_SSH_COMMAND", script)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/evil.sock")

	// a repository hook must stay dormant too
	hook := filepath.Join(r.dir, ".git", "hooks", "reference-transaction")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.commit(map[string]string{"web/compose.yaml": "c\n"})
	if _, err := r.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("caller configuration or a hook was executed")
	}
}

func TestOutputLimitKillsGit(t *testing.T) {
	cfg := testConfig(t)
	script := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nhead -c 9000000 /dev/zero\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.test.gitPath = script
	r, _ := Open(cfg, filepath.Join(t.TempDir(), "c"))
	if _, err := r.run(context.Background(), false, maxStdout, "status"); !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("stdout: err = %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nhead -c 200000 /dev/zero >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.run(context.Background(), false, maxStdout, "status"); !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("stderr: err = %v", err)
	}
}

func TestTimeoutKillsProcessGroup(t *testing.T) {
	cfg := testConfig(t)
	cfg.Timeout = 300 * time.Millisecond
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := filepath.Join(t.TempDir(), "git")
	body := "#!/bin/sh\nsleep 30 &\necho $! > " + pidFile + "\nwait\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.test.gitPath = script
	r, _ := Open(cfg, filepath.Join(t.TempDir(), "c"))
	start := time.Now()
	_, err := r.run(context.Background(), false, maxStdout, "status")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v; want deadline exceeded", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("run did not stop at the timeout")
	}
	pid, _ := os.ReadFile(pidFile)
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat("/proc/" + strings.TrimSpace(string(pid))); err == nil {
		if st, _ := os.ReadFile("/proc/" + strings.TrimSpace(string(pid)) + "/stat"); !strings.Contains(string(st), ") Z ") {
			t.Fatal("child of git survived the timeout")
		}
	}
}

func TestCancelledContext(t *testing.T) {
	f := newRemote(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Clone(ctx, testConfig(t), f.bare, filepath.Join(t.TempDir(), "c")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want canceled", err)
	}
}

func TestConfigPathsAreChecked(t *testing.T) {
	for _, p := range []string{"", "rel/key", "/a b/key", "/a'b/key", "/a/$HOME/key", "/a/../b", "/a%d/key", "/a;b"} {
		if _, err := Open(Config{KeyFile: p, KnownHosts: "/ok/known_hosts"}, "/x"); err == nil {
			t.Errorf("accepted key path %q", p)
		}
	}
	if _, err := Open(Config{KeyFile: "/var/lib/qdeploy/deploy-key", KnownHosts: "/var/lib/qdeploy/known_hosts"}, "/x"); err != nil {
		t.Fatal(err)
	}
}

func TestFailureTextRedactsUserinfo(t *testing.T) {
	got := failureText(errors.New("exit 128"), "fatal: unable to access 'https://user:tok@git.example.test/x.git/':\n boom")
	if strings.Contains(got, "tok") || strings.Contains(got, "\n") {
		t.Fatalf("got %q", got)
	}
	if got := failureText(errors.New("exit 1"), ""); got != "exit 1" {
		t.Fatalf("got %q", got)
	}
	if got := failureText(errors.New("x"), strings.Repeat("a", 2000)); len(got) > maxErrorText+3 {
		t.Fatalf("len = %d", len(got))
	}
}
