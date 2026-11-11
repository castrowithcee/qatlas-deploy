package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
)

// hostDouble stands in for the host: a temporary root for /etc, a recorded systemctl and a service that
// answers at once.
type hostDouble struct {
	root string
	// inactive makes 'systemctl is-active' fail, as for a stopped service.
	inactive bool
	cmds     []string
	waited   int
	chowned  bool
	// readOnlyFS makes every path lie on a read-only file system; checked collects the paths asked.
	readOnlyFS bool
	checked    []string
}

func useFolderHost(t *testing.T) *hostDouble {
	t.Helper()
	d := &hostDouble{root: t.TempDir()}
	old := folders
	folders = folderHost{
		euid: func() int { return 0 },
		root: d.root,
		systemctl: func(_ context.Context, args ...string) error {
			d.cmds = append(d.cmds, strings.Join(args, " "))
			if d.inactive && args[0] == "is-active" {
				return errors.New("inactive")
			}
			return nil
		},
		owner: func() (int, int, error) { d.chowned = true; return os.Getuid(), os.Getgid(), nil },
		waitService: func(context.Context) error {
			d.waited++
			return nil
		},
		readOnly: func(path string) (bool, error) {
			d.checked = append(d.checked, path)
			return d.readOnlyFS, nil
		},
	}
	t.Cleanup(func() { folders = old })
	return d
}

func (d *hostDouble) dropIn(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(d.root, stackFoldersDropIn))
	if err != nil {
		return ""
	}
	return string(data)
}

func TestRepoSetRequiresTheStackFolder(t *testing.T) {
	e := startRepoService(t)
	code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--fingerprint", e.srv.Key().Fingerprint)
	if code != 2 || !strings.Contains(stderr, "--dir is required") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}

func TestRepoSetWithoutConfirmationLeavesAMissingFolderUncreated(t *testing.T) {
	e := startRepoService(t)
	d := e.host
	missing := filepath.Join(e.dir, "new", "stacks")
	useStdin(t, strings.NewReader(""))
	code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", missing, "--fingerprint", e.srv.Key().Fingerprint)
	if code == 0 || !strings.Contains(stderr, missing) || !strings.Contains(stderr, "--create") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if _, err := os.Lstat(missing); err == nil || d.dropIn(t) != "" || len(d.cmds) != 0 || e.git.Clones() != 0 {
		t.Fatalf("a refused folder had an effect: %v, %q, %v", err, d.dropIn(t), d.cmds)
	}
}

func TestRepoSetCreateMakesTheFolderAndTheDropIn(t *testing.T) {
	e := startRepoService(t)
	d := e.host
	missing := filepath.Join(e.dir, "new", "stacks")
	code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", missing, "--create", "--fingerprint", e.srv.Key().Fingerprint)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	info, err := os.Stat(missing)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 || !d.chowned {
		t.Fatalf("folder %v, %v, chowned %v", info, err, d.chowned)
	}
	if want := "[Service]\nReadWritePaths=-" + missing + "\n"; d.dropIn(t) != want {
		t.Fatalf("drop-in = %q, want %q", d.dropIn(t), want)
	}
	if got := strings.Join(d.cmds, ";"); got != "daemon-reload;restart qdeploy" || d.waited != 1 {
		t.Fatalf("commands %q, waits %d", got, d.waited)
	}
	if e.git.Clones() != 1 {
		t.Fatalf("clones = %d", e.git.Clones())
	}
}

func TestRepoSetConfirmedAtTheTerminalCreatesTheFolder(t *testing.T) {
	e := startRepoService(t)
	missing := filepath.Join(e.dir, "answered")
	useTerminalStdin(t, "y\n")
	code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", missing, "--fingerprint", e.srv.Key().Fingerprint)
	if code != 0 || !strings.Contains(stderr, missing) {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(missing); err != nil {
		t.Fatal(err)
	}
}

func TestRepoSetDeclinedAtTheTerminalCreatesNothing(t *testing.T) {
	e := startRepoService(t)
	d := e.host
	missing := filepath.Join(e.dir, "declined")
	useTerminalStdin(t, "n\n")
	code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", missing, "--fingerprint", e.srv.Key().Fingerprint)
	if code == 0 || !strings.Contains(stderr, "nothing was created") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if _, err := os.Lstat(missing); err == nil || d.dropIn(t) != "" {
		t.Fatal("a declined folder had an effect")
	}
}

func TestDropInOnlyGrowsAndKeepsOtherLines(t *testing.T) {
	d := useFolderHost(t)
	path := filepath.Join(d.root, stackFoldersDropIn)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Service]\nReadWritePaths=-/srv/first-example\nNice=5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := folders.grant(context.Background(), dir, false, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	want := "[Service]\nReadWritePaths=-/srv/first-example\nNice=5\nReadWritePaths=-" + dir + "\n"
	if d.dropIn(t) != want {
		t.Fatalf("drop-in = %q, want %q", d.dropIn(t), want)
	}
	// A folder that is listed already changes nothing and restarts nothing.
	d.cmds = nil
	if err := folders.grant(context.Background(), dir, false, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if d.dropIn(t) != want || len(d.cmds) != 0 {
		t.Fatalf("second grant changed things: %q, %v", d.dropIn(t), d.cmds)
	}
}

func TestGrantNeedsRoot(t *testing.T) {
	d := useFolderHost(t)
	folders.euid = func() int { return 1000 }
	if err := folders.grant(context.Background(), t.TempDir(), false, &strings.Builder{}); err == nil || d.dropIn(t) != "" {
		t.Fatalf("grant without root: %v", err)
	}
}

func TestRepairBoundFolderWritesTheMissingDropInOnce(t *testing.T) {
	e := startRepoService(t)
	d := e.host
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	// An installation from before the drop-in: bound, but nothing listed.
	if err := os.RemoveAll(filepath.Join(d.root, "etc")); err != nil {
		t.Fatal(err)
	}
	d.cmds, d.waited = nil, 0
	code, _, stderr := run(t, "status")
	if code != 0 || !strings.Contains(stderr, "Notice") || !strings.Contains(stderr, e.dir) {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if want := "[Service]\nReadWritePaths=-" + e.dir + "\n"; d.dropIn(t) != want {
		t.Fatalf("drop-in = %q, want %q", d.dropIn(t), want)
	}
	if got := strings.Join(d.cmds, ";"); got != "daemon-reload;try-restart qdeploy;is-active --quiet qdeploy" || d.waited != 1 {
		t.Fatalf("commands %q, waits %d", got, d.waited)
	}
	d.cmds = nil
	if code, _, stderr := run(t, "status"); code != 0 || strings.Contains(stderr, "Notice") || len(d.cmds) != 0 {
		t.Fatalf("second run repeated the repair: %d %q %v", code, stderr, d.cmds)
	}
}

func TestRepairBoundFolderIsInertWithoutRoot(t *testing.T) {
	e := startRepoService(t)
	d := e.host
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	_ = os.RemoveAll(filepath.Join(d.root, "etc"))
	folders.euid = func() int { return 1000 }
	d.cmds = nil
	if code, _, _ := run(t, "status"); code != 0 || d.dropIn(t) != "" || len(d.cmds) != 0 {
		t.Fatalf("repair without root: %q %v", d.dropIn(t), d.cmds)
	}
}

func TestWaitForServiceReturnsWhenTheSocketAnswers(t *testing.T) {
	startRepoService(t)
	if err := waitForService(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForServiceGivesUpWithoutASocket(t *testing.T) {
	useEndpoint(t, channeltest.Endpoint(t))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := waitForService(ctx); err == nil {
		t.Fatal("no service answered, yet the wait succeeded")
	}
}

func TestRepairBoundFolderLeavesAStoppedServiceStopped(t *testing.T) {
	e := startRepoService(t)
	d := e.host
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	_ = os.RemoveAll(filepath.Join(d.root, "etc"))
	d.cmds, d.waited, d.inactive = nil, 0, true
	code, _, stderr := run(t, "status")
	if code != 0 || !strings.Contains(stderr, "grant takes effect at its next start") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if want := "[Service]\nReadWritePaths=-" + e.dir + "\n"; d.dropIn(t) != want {
		t.Fatalf("drop-in = %q", d.dropIn(t))
	}
	if got := strings.Join(d.cmds, ";"); got != "daemon-reload;try-restart qdeploy;is-active --quiet qdeploy" || d.waited != 0 {
		t.Fatalf("commands %q, waits %d", got, d.waited)
	}
	for _, c := range d.cmds {
		if strings.HasPrefix(c, "start") || strings.HasPrefix(c, "restart") {
			t.Fatalf("a stopped service was started: %q", c)
		}
	}
}
