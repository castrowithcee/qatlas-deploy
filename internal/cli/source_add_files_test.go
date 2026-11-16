package cli

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// filesTree is a folder of files without a repository: two listed files, one in a subfolder, a large file the
// first commit leaves out, a bind-mount folder with a file, and a symbolic link to a file outside.
type filesTree struct {
	dir, outside string
}

func newFilesTree(t *testing.T) filesTree {
	t.Helper()
	root, err := os.MkdirTemp("/var/tmp", "qdeploy-files-")
	if err != nil {
		t.Skip("no /var/tmp:", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	dir := filepath.Join(root, "web")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	put(t, outside, 0o600)
	put(t, filepath.Join(dir, "compose.yaml"), 0o640)
	put(t, filepath.Join(dir, ".env"), 0o640)
	put(t, filepath.Join(dir, "conf", "app.yaml"), 0o640)
	put(t, filepath.Join(dir, "big.bin"), 0o600)
	put(t, filepath.Join(dir, "data", "seed.txt"), 0o640)
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return filesTree{dir: dir, outside: outside}
}

func (f filesTree) snapshot(t *testing.T) map[string]fs.FileMode {
	t.Helper()
	got := map[string]fs.FileMode{}
	_ = filepath.WalkDir(f.dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil {
			got[path] = modeOf(t, path)
		}
		return nil
	})
	return got
}

func (f filesTree) hasGit() bool {
	_, err := os.Lstat(filepath.Join(f.dir, ".git"))
	return err == nil
}

func listedFilesInspect() control.SourceInspectResult {
	return control.SourceInspectResult{
		Case:         control.SourceCaseFiles,
		Access:       &control.SourceAccess{Mode: "0750", SetsPermissions: true},
		PlannedFiles: []string{".env", "compose.yaml", "conf/app.yaml"},
		Excluded: []control.SourceExcluded{
			{Path: "big.bin", Reason: control.SourceExcludedTooLarge},
			{Path: "data", Reason: control.SourceExcludedBindMount, Stack: "demo"}},
		Stacks: []control.SourceStack{{Name: "demo", BindMounts: []string{"data"}}},
	}
}

func TestSourceAddFilesWithoutConfirmationChangesNothing(t *testing.T) {
	d := useFolderHost(t)
	tree := newFilesTree(t)
	svc := startFakeService(t, addHandlers(listedFilesInspect()))
	useStdin(t, strings.NewReader(""))
	before := tree.snapshot(t)

	code, stdout, stderr := run(t, "source", "add", tree.dir)
	if code != 2 || stdout != "" || !strings.Contains(stderr, "confirmation-required") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for _, want := range []string{
		"Case files", "Start a git repository", "branch main", "core.sharedRepository=group",
		"first commit is made as qdeploy", "no file in the folder is written", "takes 3 files", "  compose.yaml",
		"big.bin: too-large", "data: bind-mount (stack demo)", "only in .git/info/exclude, never in .gitignore",
		"paths get the group", "listed files: group rw", "bind-mount sources and everything below: data",
		"files that are not listed stay as they are",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("preview lacks %q:\n%s", want, stderr)
		}
	}
	if !equalModes(before, tree.snapshot(t)) || tree.hasGit() || len(d.lchowns) != 0 || d.dropIn(t) != "" || len(d.cmds) != 0 ||
		svc.count(control.SourceAddCommand) != 0 {
		t.Fatal("an unconfirmed source add changed something")
	}
}

func TestSourceAddFilesPreviewLimitsLongLists(t *testing.T) {
	useFolderHost(t)
	tree := newFilesTree(t)
	inspect := listedFilesInspect()
	inspect.PlannedFiles = nil
	for i := 0; i < 60; i++ {
		inspect.PlannedFiles = append(inspect.PlannedFiles, "compose.yaml")
	}
	startFakeService(t, addHandlers(inspect))
	useStdin(t, strings.NewReader(""))
	_, _, stderr := run(t, "source", "add", tree.dir)
	if !strings.Contains(stderr, "takes 60 files") || !strings.Contains(stderr, "... and 10 more") ||
		strings.Count(stderr, "  compose.yaml") != 50 {
		t.Fatalf("the list is not limited to 50:\n%s", stderr)
	}
}

func TestSourceAddFilesStopsBeforeAnyChangeWhenTheListChanged(t *testing.T) {
	d := useFolderHost(t)
	tree := newFilesTree(t)
	var asked atomic.Int32
	handlers := addHandlers(listedFilesInspect())
	handlers[control.SourceInspectCommand] = func(raw json.RawMessage) any {
		res := listedFilesInspect()
		if asked.Add(1) > 1 {
			res.PlannedFiles = append(res.PlannedFiles, "new.yaml")
			res.Excluded = res.Excluded[1:]
		}
		return inspectWith(res)(raw)
	}
	svc := startFakeService(t, handlers)
	useStdin(t, strings.NewReader(""))
	before := tree.snapshot(t)

	code, stdout, stderr := run(t, "source", "add", tree.dir, "--yes")
	if code == 0 || stdout != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for _, want := range []string{"changed since the list was shown", "new in files: new.yaml",
		"no longer in left out: big.bin: too-large", "nothing was changed", "again"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if asked.Load() != 2 {
		t.Fatalf("inspect calls = %d, want 2", asked.Load())
	}
	if !equalModes(before, tree.snapshot(t)) || tree.hasGit() || len(d.lchowns) != 0 || d.dropIn(t) != "" || len(d.cmds) != 0 ||
		svc.count(control.SourceAddCommand) != 0 {
		t.Fatal("a changed list did not stop source add before the first change")
	}
}

func TestSourceAddFilesWithYesPassesTheConfirmedList(t *testing.T) {
	d := useFolderHost(t)
	tree := newFilesTree(t)
	var got control.SourceAddArgs
	handlers := addHandlers(listedFilesInspect())
	handlers[control.SourceAddCommand] = func(raw json.RawMessage) any {
		_ = json.Unmarshal(raw, &got)
		return control.SourceAddResult{ID: "src-1", Path: got.Path, Kind: control.SourceKindLocal, Head: "abc1234", Files: len(got.Files)}
	}
	svc := startFakeService(t, handlers)
	useStdin(t, strings.NewReader(""))
	before := tree.snapshot(t)

	code, stdout, stderr := run(t, "source", "add", tree.dir, "--yes", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout, err)
	}
	if res["id"] != "src-1" || res["kind"] != "local" || res["head"] != "abc1234" || res["files"] != float64(3) || res["changed_paths"] != float64(5) {
		t.Fatalf("result = %v", res)
	}
	if svc.count(control.SourceAddCommand) != 1 || got.Path != tree.dir || strings.Join(got.Files, ",") != ".env,compose.yaml,conf/app.yaml" {
		t.Fatalf("source.add args %+v, calls %d", got, svc.count(control.SourceAddCommand))
	}
	if !strings.Contains(d.dropIn(t), "ReadWritePaths=-"+tree.dir) || strings.Join(d.cmds, ";") != "daemon-reload;restart qdeploy" {
		t.Fatalf("drop-in %q, commands %v", d.dropIn(t), d.cmds)
	}
	if tree.hasGit() {
		t.Fatal("the client created .git; only the service starts the repository")
	}
	after := tree.snapshot(t)
	for _, f := range []string{"compose.yaml", ".env", "conf/app.yaml"} {
		path := filepath.Join(tree.dir, f)
		if after[path] != before[path]|0o060 {
			t.Errorf("%s: mode %v, was %v", f, after[path], before[path])
		}
	}
	for _, f := range []string{"big.bin", "data", "data/seed.txt", "link"} {
		path := filepath.Join(tree.dir, f)
		if after[path] != before[path] {
			t.Errorf("%s changed: %v, was %v", f, after[path], before[path])
		}
	}
	for _, call := range d.lchowns {
		if call.uid != -1 {
			t.Fatalf("the owner was passed: %+v", call)
		}
	}
}

func TestSourceAddFilesRefusalOfTheServiceKeepsThePermissionsAndSaysSo(t *testing.T) {
	d := useFolderHost(t)
	tree := newFilesTree(t)
	handlers := addHandlers(listedFilesInspect())
	handlers[control.SourceAddCommand] = func(json.RawMessage) any {
		return &control.Error{Code: "conflict", Message: "the list does not match the folder"}
	}
	startFakeService(t, handlers)
	code, stdout, stderr := run(t, "source", "add", tree.dir, "--yes")
	if code == 0 || stdout != "" || !strings.Contains(stderr, "the list does not match the folder") ||
		!strings.Contains(stderr, "5 paths of the listed files stay changed") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if len(d.lchowns) == 0 || tree.hasGit() {
		t.Fatal("unexpected state after the refusal")
	}
}

func TestSourceAddFilesThatServiceCanWriteSetsNoPermissions(t *testing.T) {
	d := useFolderHost(t)
	tree := newFilesTree(t)
	inspect := listedFilesInspect()
	inspect.Access.SetsPermissions = false
	svc := startFakeService(t, addHandlers(inspect))
	before := tree.snapshot(t)
	code, _, stderr := run(t, "source", "add", tree.dir, "--yes")
	if code != 0 || !equalModes(before, tree.snapshot(t)) || len(d.lchowns) != 0 || strings.Contains(stderr, "paths get the group") ||
		svc.count(control.SourceAddCommand) != 1 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}

func TestPlanListedFilesChangesOnlyListedFilesAndTheirFolders(t *testing.T) {
	d := useFolderHost(t)
	tree := newFilesTree(t)
	gid := otherGroup()
	before := tree.snapshot(t)
	outsideBefore := modeOf(t, tree.outside)

	plan, err := planListedFiles(tree.dir, gid,
		[]string{"compose.yaml", ".env", "conf/app.yaml", "data/seed.txt", "link", "../outside.txt", "missing.txt"}, []string{"data"})
	if err != nil {
		t.Fatal(err)
	}
	if !equalModes(before, tree.snapshot(t)) || len(d.lchowns) != 0 {
		t.Fatal("planning changed the tree")
	}
	if plan.changed() != 5 || plan.links != 1 || plan.repo || len(plan.mounts) != 1 {
		t.Fatalf("plan: %d changed, %d links, repo %v, mounts %v", plan.changed(), plan.links, plan.repo, plan.mounts)
	}
	if err := folders.applyPermissions(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if tree.hasGit() {
		t.Fatal("a repository was created")
	}
	for _, f := range []string{"compose.yaml", ".env", "conf/app.yaml"} {
		path := filepath.Join(tree.dir, f)
		if modeOf(t, path) != before[path]|0o060 || gidOf(t, path) != gid {
			t.Errorf("%s: mode %v gid %d", f, modeOf(t, path), gidOf(t, path))
		}
	}
	for _, f := range []string{"", "conf"} {
		path := filepath.Join(tree.dir, f)
		if modeOf(t, path) != before[path]|0o070|fs.ModeSetgid || gidOf(t, path) != gid {
			t.Errorf("folder %q: mode %v gid %d", f, modeOf(t, path), gidOf(t, path))
		}
	}
	for _, f := range []string{"big.bin", "data", "data/seed.txt", "link"} {
		path := filepath.Join(tree.dir, f)
		if modeOf(t, path) != before[path] || (f != "link" && gidOf(t, path) != os.Getgid()) {
			t.Errorf("%s changed: mode %v, was %v", f, modeOf(t, path), before[path])
		}
	}
	if modeOf(t, tree.outside) != outsideBefore || gidOf(t, tree.outside) != os.Getgid() {
		t.Fatal("the target of the symbolic link changed")
	}
	for _, call := range d.lchowns {
		if call.uid != -1 || call.gid != gid || !strings.HasPrefix(call.path, tree.dir) {
			t.Fatalf("lchown %+v: only the group inside the folder may change", call)
		}
	}
}
