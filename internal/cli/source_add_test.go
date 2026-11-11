package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// fakeHandlers answers service commands; a command without a handler gets no answer.
type fakeHandlers map[string]func(raw json.RawMessage) any

type fakeService struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeService) count(command string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == command {
			n++
		}
	}
	return n
}

func startFakeService(t *testing.T, handlers fakeHandlers) *fakeService {
	t.Helper()
	ep := channeltest.Endpoint(t)
	useEndpoint(t, ep)
	l, err := ep.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	f := &fakeService{}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				raw, err := control.ReadFrame(bufio.NewReader(conn), control.MaxRequestBytes)
				if err != nil {
					return
				}
				var req control.Request
				if json.Unmarshal(raw, &req) != nil {
					return
				}
				h := handlers[req.Command]
				if h == nil {
					return
				}
				f.mu.Lock()
				f.calls = append(f.calls, req.Command)
				f.mu.Unlock()
				result, _ := json.Marshal(h(req.Args))
				_ = control.WriteFrame(conn, control.Response{V: control.Version, OK: true, Result: result}, control.MaxResponseBytes)
			}(conn)
		}
	}()
	channeltest.WaitReady(t, ep)
	return f
}

func inspectWith(res control.SourceInspectResult) func(json.RawMessage) any {
	return func(raw json.RawMessage) any {
		var a control.SourceInspectArgs
		_ = json.Unmarshal(raw, &a)
		res.Path = a.Path
		if res.Stacks == nil {
			res.Stacks = []control.SourceStack{}
		}
		return res
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir=" + filepath.Join(dir, ".git"), "--work-tree=" + dir}, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func put(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func modeOf(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}

type permTree struct {
	dir, outside string
}

// newPermTree builds a repository with versioned files, a bind-mount source that holds a versioned file,
// an unversioned secrets file and folder, and a versioned symbolic link to a file outside the tree.
func newPermTree(t *testing.T) permTree {
	t.Helper()
	// /var/tmp, like the service tests: /tmp may be a repository of its own on the dev host.
	root, err := os.MkdirTemp("/var/tmp", "qdeploy-perms-")
	if err != nil {
		t.Skip("no /var/tmp:", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	dir := filepath.Join(root, "stacks")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(outside, "target.txt")
	put(t, target, 0o600)
	gitIn(t, dir, "init", "-q")
	put(t, filepath.Join(dir, "top.txt"), 0o640)
	put(t, filepath.Join(dir, "demo", "compose.yaml"), 0o640)
	put(t, filepath.Join(dir, "demo", "data", "seed.txt"), 0o640)
	put(t, filepath.Join(dir, "demo", "run.sh"), 0o750)
	put(t, filepath.Join(dir, "demo", "secrets.env"), 0o600)
	put(t, filepath.Join(dir, "notes", "private.txt"), 0o600)
	if err := os.Symlink(target, filepath.Join(dir, "demo", "link")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "top.txt", "demo/compose.yaml", "demo/data/seed.txt", "demo/run.sh", "demo/link")
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return permTree{dir: dir, outside: target}
}

func (p permTree) snapshot(t *testing.T) map[string]fs.FileMode {
	t.Helper()
	got := map[string]fs.FileMode{}
	_ = filepath.WalkDir(p.dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !strings.Contains(path, "/.git") {
			got[path] = modeOf(t, path)
		}
		return nil
	})
	return got
}

// otherGroup is a group of the test user that differs from the primary one, so the change is visible.
func otherGroup() int {
	groups, _ := os.Getgroups()
	for _, g := range groups {
		if g != os.Getgid() {
			return g
		}
	}
	return os.Getgid()
}

func gidOf(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Sys().(*syscall.Stat_t).Gid)
}

func TestPermissionsPlanAndApplyTouchOnlyVersionedPaths(t *testing.T) {
	d := useFolderHost(t)
	tree := newPermTree(t)
	gid := otherGroup()
	before := tree.snapshot(t)
	outsideBefore := modeOf(t, tree.outside)

	plan, err := planPermissions(context.Background(), tree.dir, gid, []string{"demo/data"})
	if err != nil {
		t.Fatal(err)
	}
	if !equalModes(before, tree.snapshot(t)) || len(d.lchowns) != 0 {
		t.Fatal("planning changed the tree")
	}
	if len(plan.mounts) != 1 || plan.links != 1 {
		t.Fatalf("mounts %v, links %d", plan.mounts, plan.links)
	}
	if err := d.applyPermissionsForTest(t, plan); err != nil {
		t.Fatal(err)
	}

	changedFiles := []string{"top.txt", "demo/compose.yaml", "demo/run.sh"}
	for _, f := range changedFiles {
		path := filepath.Join(tree.dir, f)
		if want := before[path] | 0o060; modeOf(t, path) != want || gidOf(t, path) != gid {
			t.Errorf("%s: mode %v gid %d, want %v gid %d", f, modeOf(t, path), gidOf(t, path), want, gid)
		}
	}
	for _, f := range []string{"", "demo"} {
		path := filepath.Join(tree.dir, f)
		want := (before[path] | 0o070 | fs.ModeSetgid)
		if modeOf(t, path) != want || gidOf(t, path) != gid {
			t.Errorf("folder %q: mode %v gid %d, want %v gid %d", f, modeOf(t, path), gidOf(t, path), want, gid)
		}
	}
	gitDir := filepath.Join(tree.dir, ".git")
	if m := modeOf(t, gitDir); m&fs.ModeSetgid == 0 || m.Perm()&0o070 != 0o070 || gidOf(t, gitDir) != gid {
		t.Errorf(".git: mode %v gid %d", m, gidOf(t, gitDir))
	}
	if m := modeOf(t, filepath.Join(tree.dir, ".git", "config")); m.Perm()&0o060 != 0o060 || gidOf(t, filepath.Join(tree.dir, ".git", "config")) != gid {
		t.Errorf(".git/config: mode %v", m)
	}

	// Everything else is as it was: bind mount and what lies below, unversioned paths, the link and its target.
	for _, f := range []string{"demo/data", "demo/data/seed.txt", "demo/secrets.env", "notes", "notes/private.txt", "demo/link"} {
		path := filepath.Join(tree.dir, f)
		if modeOf(t, path) != before[path] || (f != "demo/link" && gidOf(t, path) != os.Getgid()) {
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
	if got := gitConfigIn(t, tree.dir, "core.sharedRepository"); got != "group" {
		t.Fatalf("core.sharedRepository = %q", got)
	}
}

func equalModes(a, b map[string]fs.FileMode) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func gitConfigIn(t *testing.T, dir, key string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir="+filepath.Join(dir, ".git"), "config", "--local", key).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func (d *hostDouble) applyPermissionsForTest(t *testing.T, plan *permPlan) error {
	t.Helper()
	return folders.applyPermissions(context.Background(), plan)
}

func TestApplyPermissionsSkipsAPathBehindASymbolicLink(t *testing.T) {
	useFolderHost(t)
	tree := newPermTree(t)
	secret := filepath.Join(filepath.Dir(tree.outside), "other.txt")
	put(t, secret, 0o600)
	outsideDir := filepath.Dir(tree.outside)
	dirBefore := modeOf(t, outsideDir)
	// A folder that became a link after the plan was made.
	if err := os.Symlink(filepath.Dir(tree.outside), filepath.Join(tree.dir, "swapped")); err != nil {
		t.Fatal(err)
	}
	plan := &permPlan{dir: tree.dir, gid: os.Getgid(),
		dirs:  []string{filepath.Join(tree.dir, "swapped")},
		files: []string{filepath.Join(tree.dir, "swapped", "other.txt"), filepath.Join(tree.dir, "demo", "link")}}
	if err := folders.applyPermissions(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if modeOf(t, secret) != 0o600 || modeOf(t, tree.outside) != 0o600 || modeOf(t, outsideDir) != dirBefore {
		t.Fatal("a path behind a symbolic link was changed")
	}
}

func localInspect(sets bool) control.SourceInspectResult {
	gw := !sets
	return control.SourceInspectResult{Case: control.SourceCaseLocal, Access: &control.SourceAccess{
		Mode: "0750", ServiceCanWrite: !sets, SetsPermissions: sets, GitWritable: &gw},
		Stacks: []control.SourceStack{{Name: "demo", BindMounts: []string{"demo/data"}}}}
}

func addHandlers(inspect control.SourceInspectResult) fakeHandlers {
	return fakeHandlers{
		control.SourceInspectCommand: inspectWith(inspect),
		control.SourceAddCommand: func(raw json.RawMessage) any {
			var a control.SourceAddArgs
			_ = json.Unmarshal(raw, &a)
			return control.SourceAddResult{ID: "src-1", Path: a.Path, Kind: control.SourceKindLocal, Head: "abc1234"}
		},
	}
}

func TestSourceAddWithoutConfirmationChangesNothing(t *testing.T) {
	d := useFolderHost(t)
	tree := newPermTree(t)
	svc := startFakeService(t, addHandlers(localInspect(true)))
	useStdin(t, strings.NewReader(""))
	before := tree.snapshot(t)

	code, stdout, stderr := run(t, "source", "add", tree.dir)
	if code != 2 || stdout != "" || !strings.Contains(stderr, "confirmation-required") || !strings.Contains(stderr, "--yes") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for _, want := range []string{"Case local", "paths get the group", "the owner stays", "demo/data", "unversioned files stay as they are"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("preview lacks %q:\n%s", want, stderr)
		}
	}
	if !equalModes(before, tree.snapshot(t)) || len(d.lchowns) != 0 || d.dropIn(t) != "" || len(d.cmds) != 0 || svc.count(control.SourceAddCommand) != 0 {
		t.Fatal("an unconfirmed source add changed something")
	}
}

func TestSourceAddLocalWithYesGrantsAndBinds(t *testing.T) {
	d := useFolderHost(t)
	tree := newPermTree(t)
	svc := startFakeService(t, addHandlers(localInspect(true)))
	useStdin(t, strings.NewReader(""))

	code, stdout, stderr := run(t, "source", "add", tree.dir, "--yes", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout, err)
	}
	if got["id"] != "src-1" || got["path"] != tree.dir || got["kind"] != "local" || got["head"] != "abc1234" || got["changed_paths"].(float64) < 5 {
		t.Fatalf("result = %v", got)
	}
	if svc.count(control.SourceAddCommand) != 1 {
		t.Fatalf("source.add calls = %d", svc.count(control.SourceAddCommand))
	}
	if !strings.Contains(d.dropIn(t), "ReadWritePaths=-"+tree.dir) || strings.Join(d.cmds, ";") != "daemon-reload;restart qdeploy" {
		t.Fatalf("drop-in %q, commands %v", d.dropIn(t), d.cmds)
	}
	if modeOf(t, tree.dir)&fs.ModeSetgid == 0 || gitConfigIn(t, tree.dir, "core.sharedRepository") != "group" {
		t.Fatal("permissions were not set")
	}
	for _, call := range d.lchowns {
		if call.uid != -1 {
			t.Fatalf("the owner was passed: %+v", call)
		}
	}
}

func TestSourceAddLocalThatServiceCanWriteSetsNoPermissions(t *testing.T) {
	d := useFolderHost(t)
	tree := newPermTree(t)
	startFakeService(t, addHandlers(localInspect(false)))
	before := tree.snapshot(t)
	code, _, stderr := run(t, "source", "add", tree.dir, "--yes")
	if code != 0 || !equalModes(before, tree.snapshot(t)) || len(d.lchowns) != 0 || strings.Contains(stderr, "paths get the group") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if !strings.Contains(d.dropIn(t), tree.dir) {
		t.Fatal("no drop-in entry")
	}
}

func TestSourceAddRefusesRejectAndFilesWithoutChange(t *testing.T) {
	d := useFolderHost(t)
	dir := t.TempDir()
	for name, res := range map[string]control.SourceInspectResult{
		"reject": {Case: control.SourceCaseReject, Reason: "the folder is not empty", Fix: "Choose another folder."},
		"files":  filesResult(control.SourceInspectArgs{Path: dir}),
	} {
		svc := startFakeService(t, addHandlers(res))
		code, stdout, stderr := run(t, "source", "add", dir, "--yes")
		if code == 0 || stdout != "" || d.dropIn(t) != "" || len(d.cmds) != 0 || len(d.lchowns) != 0 || svc.count(control.SourceAddCommand) != 0 {
			t.Fatalf("%s: code %d, stdout %q, stderr %q", name, code, stdout, stderr)
		}
		want := "the folder is not empty"
		if name == "files" {
			want = "files without git"
		}
		if !strings.Contains(stderr, want) {
			t.Errorf("%s: stderr lacks %q: %q", name, want, stderr)
		}
	}
}

func TestSourceAddNeedsRootAndAURLForAClone(t *testing.T) {
	d := useFolderHost(t)
	svc := startFakeService(t, addHandlers(control.SourceInspectResult{Case: control.SourceCaseClone, NeedsURL: true,
		Access: &control.SourceAccess{Mode: "0755"}}))
	dir := t.TempDir()
	if code, _, stderr := run(t, "source", "add", dir, "--yes"); code != 2 || !strings.Contains(stderr, "--url") {
		t.Fatalf("clone without URL: code %d, stderr %q", code, stderr)
	}
	folders.euid = func() int { return 1000 }
	if code, _, _ := run(t, "source", "add", dir, "--yes"); code == 0 {
		t.Fatal("source add without root succeeded")
	}
	if svc.count(control.SourceInspectCommand) != 1 || d.dropIn(t) != "" {
		t.Fatalf("inspect calls %d", svc.count(control.SourceInspectCommand))
	}
	folders.euid = func() int { return 0 }
	for _, args := range [][]string{{"source", "add"}, {"source", "add", "rel/dir"}, {"source", "add", dir, "--fingerprint", "abc"}} {
		if code, _, _ := run(t, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}

func cloneHandlers(inspect control.SourceInspectResult, set *int) fakeHandlers {
	return fakeHandlers{
		control.SourceInspectCommand: inspectWith(inspect),
		control.RepoProbeCommand: func(json.RawMessage) any {
			return control.RepoProbeResult{Host: "git.example.test", Port: "22", KeyType: "ssh-ed25519", Fingerprint: "SHA256:demo-fingerprint"}
		},
		control.RepoSetCommand: func(raw json.RawMessage) any {
			*set++
			var a control.RepoSetArgs
			_ = json.Unmarshal(raw, &a)
			return control.RepoSetResult{URL: a.URL, Dir: a.Dir, Fingerprint: a.Fingerprint, Head: "abc1234", Cloned: true}
		},
	}
}

func TestSourceAddYesDoesNotConfirmTheHostKey(t *testing.T) {
	d := useFolderHost(t)
	dir := filepath.Join(t.TempDir(), "stacks")
	var sets int
	startFakeService(t, cloneHandlers(control.SourceInspectResult{Case: control.SourceCaseClone, URL: inspectURL,
		Access: &control.SourceAccess{Mode: "0755"}}, &sets))
	useStdin(t, strings.NewReader(""))
	code, _, stderr := run(t, "source", "add", dir, "--url", inspectURL, "--yes")
	if code != 2 || !strings.Contains(stderr, "SHA256:demo-fingerprint") || !strings.Contains(stderr, "--fingerprint") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if sets != 0 || d.dropIn(t) != "" {
		t.Fatal("a refused host key changed something")
	}
	if _, err := os.Lstat(dir); err == nil {
		t.Fatal("the folder was created")
	}

	code, stdout, stderr := run(t, "source", "add", dir, "--url", inspectURL, "--yes", "--fingerprint", "SHA256:demo-fingerprint", "--output", "json")
	if code != 0 || sets != 1 || !strings.Contains(stdout, `"cloned":true`) || !strings.Contains(stdout, `"changed_paths":0`) {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || !strings.Contains(d.dropIn(t), dir) {
		t.Fatalf("folder %v, drop-in %q", err, d.dropIn(t))
	}
}

func TestSourceAddAdoptCloneSetsPermissionsThenBinds(t *testing.T) {
	d := useFolderHost(t)
	tree := newPermTree(t)
	var sets int
	inspect := localInspect(true)
	inspect.Case, inspect.URL = control.SourceCaseAdopt, inspectURL
	startFakeService(t, cloneHandlers(inspect, &sets))
	code, stdout, stderr := run(t, "source", "add", tree.dir, "--yes", "--fingerprint", "SHA256:demo-fingerprint", "--output", "json")
	if code != 0 || sets != 1 || !strings.Contains(stdout, `"url":"`+inspectURL+`"`) || strings.Contains(stdout, `"changed_paths":0`) {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if len(d.lchowns) == 0 || !strings.Contains(d.dropIn(t), tree.dir) {
		t.Fatal("permissions or drop-in missing")
	}
}

func TestSourceInspectWarnsWhenGitIsNotWritable(t *testing.T) {
	useFolderHost(t)
	startFakeService(t, fakeHandlers{control.SourceInspectCommand: inspectWith(localInspect(true))})
	_, _, stderr := run(t, "source", "inspect", "/srv/demo")
	for _, want := range []string{"the service cannot write .git", "source add sets the group permissions", "sudo -u qdeploy git"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("warning lacks %q: %q", want, stderr)
		}
	}
	_, _, stderr = run(t, "source", "inspect", "/srv/demo", "--output", "json")
	if stderr != "" {
		t.Fatalf("structured output has a warning: %q", stderr)
	}
}

func TestPermissionsSkipAVersionedFileWithAFurtherHardLink(t *testing.T) {
	useFolderHost(t)
	tree := newPermTree(t)
	// A versioned file that shares its inode with a file outside the folder.
	shared := filepath.Join(filepath.Dir(tree.outside), "shared.txt")
	put(t, shared, 0o600)
	if err := os.Link(shared, filepath.Join(tree.dir, "shared.txt")); err != nil {
		t.Skip("no hard links here:", err)
	}
	gitIn(t, tree.dir, "add", "shared.txt")
	plan, err := planPermissions(context.Background(), tree.dir, os.Getgid(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.hardlinks != 1 {
		t.Fatalf("hardlinks = %d, want 1", plan.hardlinks)
	}
	// Also a file that got a further link after the plan was made stays as it is.
	plan.files = append(plan.files, filepath.Join(tree.dir, "shared.txt"))
	if err := folders.applyPermissions(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if modeOf(t, shared) != 0o600 {
		t.Fatalf("the file outside the folder changed: %v", modeOf(t, shared))
	}
}

func TestPlanFolderChangesOnlyTheFolder(t *testing.T) {
	useFolderHost(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	plan, err := planFolder(dir, os.Getgid())
	if err != nil {
		t.Fatal(err)
	}
	// No repository: applying must not run git.
	if err := folders.applyPermissions(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, dir); m.Perm() != 0o770 || m&fs.ModeSetgid == 0 {
		t.Fatalf("mode %v, want group rwx and setgid", m)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		t.Fatal("a repository was created")
	}
}
