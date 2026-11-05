package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

type fakeDatadir struct {
	dirs     []control.DirInfo
	known    bool
	dir      string
	deployed []string
}

func (f *fakeDatadir) Missing(context.Context, string) ([]control.DirInfo, bool, error) {
	return f.dirs, f.known, nil
}
func (f *fakeDatadir) StackDir(context.Context, string) (string, error) { return f.dir, nil }
func (f *fakeDatadir) Deploy(_ context.Context, stack string) (control.DeployResult, error) {
	f.deployed = append(f.deployed, stack)
	return control.DeployResult{ID: "20260101T000000Z-00000000", Stack: stack, Commit: "abc"}, nil
}

func useDatadir(t *testing.T, f *fakeDatadir, euid int) {
	t.Helper()
	oldNew, oldEuid := newDatadirAPI, datadirEuid
	newDatadirAPI = func(*cobra.Command, *notRunning) datadirAPI { return f }
	datadirEuid = func() int { return euid }
	t.Cleanup(func() { newDatadirAPI, datadirEuid = oldNew, oldEuid })
}

func newFakeDatadir(t *testing.T, owner string) *fakeDatadir {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &fakeDatadir{known: true, dir: dir, dirs: []control.DirInfo{{Path: "data/db", Owner: owner}, {Path: "logs", Owner: owner}}}
}

func selfOwner() string { return itoa(os.Getuid()) + ":" + itoa(os.Getgid()) }

func itoa(n int) string { return strconv.Itoa(n) }

func TestDatadirWithoutYesCreatesNothing(t *testing.T) {
	f := newFakeDatadir(t, selfOwner())
	useDatadir(t, f, 0)
	code, stdout, stderr := run(t, "stack", "datadir", "demo")
	if code == 0 || !strings.Contains(stderr, "--yes") || !strings.Contains(stderr, "Would create in "+f.dir) ||
		!strings.Contains(stderr, "data/db") || !strings.Contains(stderr, "logs") || !strings.Contains(stderr, "mode 0750") ||
		!strings.Contains(stderr, "owner "+selfOwner()) {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 0 || len(f.deployed) != 0 {
		t.Fatalf("created %v, deployed %v", entries, f.deployed)
	}
}

func TestDatadirWithYesCreatesAndDeploys(t *testing.T) {
	f := newFakeDatadir(t, selfOwner())
	useDatadir(t, f, 0)
	code, stdout, stderr := run(t, "stack", "datadir", "demo", "--mode", "0755", "--yes", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d %q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil || got["created"] != "logs, data/db" || got["parents"] != "data" ||
		got["mode"] != "0755" || got["run"] != "20260101T000000Z-00000000" || got["owner"] != selfOwner() {
		t.Fatalf("%q %v", stdout, err)
	}
	if fi, err := os.Stat(filepath.Join(f.dir, "data", "db")); err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("%v %v", fi, err)
	}
	if len(f.deployed) != 1 || f.deployed[0] != "demo" {
		t.Fatalf("deployed %v", f.deployed)
	}
}

func TestDatadirOwnerFlagOverridesProposal(t *testing.T) {
	f := newFakeDatadir(t, "99999:99999")
	useDatadir(t, f, 0)
	if code, _, stderr := run(t, "stack", "datadir", "demo", "--owner", selfOwner(), "--yes"); code != 0 {
		t.Fatalf("code %d %q", code, stderr)
	}
}

func TestDatadirNeedsOwner(t *testing.T) {
	f := newFakeDatadir(t, "")
	useDatadir(t, f, 0)
	code, _, stderr := run(t, "stack", "datadir", "demo", "--yes")
	if code == 0 || !strings.Contains(stderr, "--owner") {
		t.Fatalf("code %d %q", code, stderr)
	}
	f.dirs[0].Owner = "1:1"
	f.dirs[1].Owner = "2:2"
	if code, _, stderr := run(t, "stack", "datadir", "demo", "--yes"); code == 0 || !strings.Contains(stderr, "--owner") {
		t.Fatalf("differing owners: %d %q", code, stderr)
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 0 || len(f.deployed) != 0 {
		t.Fatalf("created %v deployed %v", entries, f.deployed)
	}
}

func TestDatadirNoFindingChangesNothing(t *testing.T) {
	f := newFakeDatadir(t, selfOwner())
	f.dirs = nil
	useDatadir(t, f, 0)
	code, stdout, stderr := run(t, "stack", "datadir", "demo", "--yes")
	if code != 0 || !strings.Contains(stdout, "nothing changed") || len(f.deployed) != 0 {
		t.Fatalf("code %d %q %q deployed %v", code, stdout, stderr, f.deployed)
	}
}

func TestDatadirRefusals(t *testing.T) {
	f := newFakeDatadir(t, selfOwner())
	useDatadir(t, f, 1000)
	if code, _, stderr := run(t, "stack", "datadir", "demo", "--yes"); code == 0 || !strings.Contains(stderr, "root") {
		t.Fatalf("non-root: %d %q", code, stderr)
	}
	useDatadir(t, f, 0)
	for _, args := range [][]string{
		{"--mode", "4755"}, {"--mode", "1777"}, {"--mode", "01000"}, {"--mode", "rwx"},
		{"--owner", "root"}, {"--owner", "1000"}, {"--owner", "-1:-1"},
	} {
		if code, _, _ := run(t, append([]string{"stack", "datadir", "demo", "--yes"}, args...)...); code == 0 {
			t.Errorf("%v accepted", args)
		}
	}
	f.known = false
	if code, _, stderr := run(t, "stack", "datadir", "nope", "--yes"); code == 0 || !strings.Contains(stderr, "qdeploy stack list") {
		t.Fatalf("unknown stack: %d %q", code, stderr)
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 0 || len(f.deployed) != 0 {
		t.Fatalf("created %v deployed %v", entries, f.deployed)
	}
}

func TestDatadirRefusesUntrustedPathFromService(t *testing.T) {
	f := newFakeDatadir(t, selfOwner())
	f.dirs = []control.DirInfo{{Path: "../escape", Owner: selfOwner()}}
	useDatadir(t, f, 0)
	if code, _, _ := run(t, "stack", "datadir", "demo", "--yes"); code == 0 || len(f.deployed) != 0 {
		t.Fatalf("code %d deployed %v", code, f.deployed)
	}
	if _, err := os.Lstat(filepath.Join(f.dir, "..", "escape")); err == nil {
		t.Fatal("escaped")
	}
}
