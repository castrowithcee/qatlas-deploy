package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// remoteFiles is what newRemote holds on main.
var remoteFiles = map[string]string{"README.md": "x\n", "web/compose.yaml": "a\n", "db/compose.yaml": "a\n"}

// hostRepo is a repository with a history of its own, opened in the remote mode.
func hostRepo(t *testing.T, files map[string]string) *Repo {
	t.Helper()
	if _, err := lookIn(fixedPath, "git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := filepath.Join(t.TempDir(), "stacks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, dir, "init", "-q", "-b", "main")
	localCommit(t, dir, map[string]string{"first.txt": "1\n"})
	localCommit(t, dir, files)
	r, err := Open(testConfig(t), dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// emptyBare is a remote without any commit.
func emptyBare(t *testing.T, branch string) string {
	t.Helper()
	if _, err := lookIn(fixedPath, "git"); err != nil {
		t.Skip("git is not installed")
	}
	bare := filepath.Join(t.TempDir(), "empty.git")
	fixtureGit(t, t.TempDir(), "init", "--bare", "-q", "-b", branch, bare)
	return bare
}

// snapshot records mode, mtime and content of everything below dir, .git included.
func snapshot(t *testing.T, dir string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		body := ""
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			body = string(b)
		}
		out[p] = fileState{info.Mode(), info.ModTime(), body}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func wantSame(t *testing.T, what string, before, after map[string]fileState) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("%s: %d entries before, %d after", what, len(before), len(after))
	}
	for p, b := range before {
		if a, ok := after[p]; !ok || a.mode != b.mode || !a.mtime.Equal(b.mtime) || a.body != b.body {
			t.Errorf("%s: %s changed", what, p)
		}
	}
}

func wantNoTempLeft(t *testing.T) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(os.TempDir(), "qdeploy-compare-*"))
	if len(left) > 0 {
		t.Fatalf("temporary repositories left: %v", left)
	}
}

func TestCompareRemoteOfARepository(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t)
	remoteHead := fixtureGit(t, remote.work, "rev-parse", "HEAD")

	// A remote without a branch of that name is empty, whatever else it holds.
	r := hostRepo(t, map[string]string{"web/compose.yaml": "mine\n"})
	other := emptyBare(t, "master")
	fixtureGit(t, remote.work, "push", "-q", other, "main:master")
	for _, url := range []string{emptyBare(t, "main"), other} {
		before := snapshot(t, r.dir)
		got, err := r.CompareRemote(ctx, url)
		if err != nil || got.State != RemoteEmpty || got.Branch != "main" || got.Commit != "" {
			t.Fatalf("CompareRemote(%s) = %+v, %v", url, got, err)
		}
		wantSame(t, "empty", before, snapshot(t, r.dir))
	}

	before := snapshot(t, r.dir)
	got, err := r.CompareRemote(ctx, remote.bare)
	if err != nil {
		t.Fatal(err)
	}
	want := Comparison{State: RemoteDifferent, Branch: "main", Commit: remoteHead,
		Paths: []string{"README.md", "db/compose.yaml", "first.txt", "web/compose.yaml"}, PathCount: 4}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("different: %+v, want %+v", got, want)
	}
	wantSame(t, "different", before, snapshot(t, r.dir))
	if _, err := os.Stat(filepath.Join(r.dir, ".git", "objects", "info", "alternates")); err == nil {
		t.Fatal("the working copy borrowed objects")
	}

	// The same files with another history are equal.
	same := hostRepo(t, remoteFiles)
	fixtureGit(t, same.dir, "rm", "-q", "first.txt")
	fixtureGit(t, same.dir, "commit", "-q", "-m", "drop")
	before = snapshot(t, same.dir)
	got, err = same.CompareRemote(ctx, remote.bare)
	if err != nil || got.State != RemoteEqual || got.Commit != remoteHead || len(got.Paths) != 0 || got.PathCount != 0 {
		t.Fatalf("equal: %+v, %v", got, err)
	}
	wantSame(t, "equal", before, snapshot(t, same.dir))
	wantNoTempLeft(t)

	files := map[string]string{}
	for i := range MaxListedPaths + 5 {
		files[fmt.Sprintf("many/f%03d.txt", i)] = "x\n"
	}
	many := hostRepo(t, files)
	if got, err := many.CompareRemote(ctx, remote.bare); err != nil || got.PathCount < MaxListedPaths+5 || len(got.Paths) != MaxListedPaths ||
		!slices.IsSorted(got.Paths) {
		t.Fatalf("many: %d paths of %d, %v", len(got.Paths), got.PathCount, err)
	}

	for _, bad := range []string{"-x", "", "/does/not/exist.git", "ext::sh -c x"} {
		if _, err := r.CompareRemote(ctx, bad); err == nil {
			t.Errorf("CompareRemote(%q) succeeded", bad)
		}
	}
	local, _ := Open(Config{Local: true}, r.dir)
	if _, err := local.CompareRemote(ctx, remote.bare); err == nil {
		t.Fatal("compared in local mode")
	}
}

func TestCompareFolderOfFilesWithoutGit(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t)
	remoteHead := fixtureGit(t, remote.work, "rev-parse", "HEAD")
	folder := func(files map[string]string) string {
		dir := filepath.Join(t.TempDir(), "stacks")
		writeTree(t, dir, files)
		return dir
	}
	cfg := testConfig(t)

	dir := folder(map[string]string{"README.md": "x\n", "web/compose.yaml": "a\n", "db/compose.yaml": "a\n",
		"web/secrets.env": "DEMO_TOKEN=not-a-real-secret\n", ".gitattributes": "* text eol=crlf\n"})
	files := []string{"README.md", "db/compose.yaml", "web/compose.yaml"}
	before := snapshot(t, dir)
	got, err := CompareFolder(ctx, cfg, dir, remote.bare, files)
	if err != nil || got.State != RemoteEqual || got.Branch != "main" || got.Commit != remoteHead {
		t.Fatalf("equal: %+v, %v", got, err)
	}
	got, err = CompareFolder(ctx, cfg, dir, remote.bare, []string{"README.md", "web/compose.yaml", "web/secrets.env"})
	want := Comparison{State: RemoteDifferent, Branch: "main", Commit: remoteHead,
		Paths: []string{"db/compose.yaml", "web/secrets.env"}, PathCount: 2}
	if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("different: %+v, %v; want %+v", got, err, want)
	}
	wantSame(t, "folder", before, snapshot(t, dir))
	if _, err := os.Lstat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("the comparison left a .git: %v", err)
	}

	// The default branch of the remote counts, "main" only if it names none.
	master := emptyBare(t, "master")
	fixtureGit(t, remote.work, "push", "-q", master, "main:master")
	if got, err := CompareFolder(ctx, cfg, dir, master, files); err != nil || got.State != RemoteEqual || got.Branch != "master" {
		t.Fatalf("master: %+v, %v", got, err)
	}
	if got, err := CompareFolder(ctx, cfg, dir, emptyBare(t, "trunk"), files); err != nil || got.State != RemoteEmpty || got.Branch != "main" {
		t.Fatalf("empty: %+v, %v", got, err)
	}
	wantSame(t, "folder", before, snapshot(t, dir))
	wantNoTempLeft(t)

	for name, c := range map[string]struct {
		dir   string
		files []string
	}{
		"no files":  {dir, nil},
		"vanished":  {dir, []string{"README.md", "gone.txt"}},
		"outside":   {dir, []string{"../x"}},
		"has a git": {func() string { d := folder(map[string]string{"a": "1"}); fixtureGit(t, d, "init", "-q"); return d }(), []string{"a"}},
	} {
		if _, err := CompareFolder(ctx, cfg, c.dir, remote.bare, c.files); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestAddRemoteFetchBranchAndLocalHistory(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t)
	remoteHead := fixtureGit(t, remote.work, "rev-parse", "HEAD")
	r := hostRepo(t, map[string]string{"web/compose.yaml": "mine\n"})
	head, _ := r.Head(ctx)

	if err := r.AddRemote(ctx, "ssh://git.example.test/demo/stacks.git"); err != nil {
		t.Fatal(err)
	}
	if url, err := r.RemoteURL(ctx); err != nil || url != "ssh://git.example.test/demo/stacks.git" {
		t.Fatalf("url = %q, %v", url, err)
	}
	if err := r.AddRemote(ctx, remote.bare); !errors.Is(err, ErrHasRemote) {
		t.Fatalf("second remote: %v", err)
	}
	fixtureGit(t, r.dir, "remote", "set-url", "origin", remote.bare)
	if err := r.FetchBranch(ctx, "nope"); err == nil {
		t.Fatal("fetched a branch the remote lacks")
	}
	if err := r.FetchBranch(ctx, "--all"); err == nil {
		t.Fatal("accepted an option as a branch")
	}
	if err := r.FetchBranch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", "refs/remotes/origin/main"); got != remoteHead {
		t.Fatalf("tracking ref = %s", got)
	}
	if h, _ := r.Head(ctx); h != head {
		t.Fatal("fetch moved HEAD")
	}
	for _, bad := range []string{"-x", "ssh://git.example.test/demo x.git"} {
		r2 := hostRepo(t, map[string]string{"a": "1\n"})
		if err := r2.AddRemote(ctx, bad); err == nil {
			t.Errorf("AddRemote(%q) succeeded", bad)
		}
	}
	local, _ := Open(Config{Local: true}, r.dir)
	if err := local.AddRemote(ctx, remote.bare); err == nil {
		t.Fatal("added a remote in local mode")
	}

	if err := r.SaveLocalHistory(ctx, head); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", LocalHistoryRef); got != head {
		t.Fatalf("saved = %s", got)
	}
	if err := r.SaveLocalHistory(ctx, remoteHead); !errors.Is(err, ErrHistorySaved) {
		t.Fatalf("second save: %v", err)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", LocalHistoryRef); got != head {
		t.Fatalf("the saved history was overwritten: %s", got)
	}
	if err := r.SaveLocalHistory(ctx, "HEAD"); err == nil {
		t.Fatal("accepted a name for a commit id")
	}
}

func TestAdoptTheRemoteHistoryWithTheTreeOfTheHost(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t)
	remoteHead := fixtureGit(t, remote.work, "rev-parse", "HEAD")
	r := hostRepo(t, map[string]string{"web/compose.yaml": "mine\n"})
	writeTree(t, r.dir, map[string]string{"web/compose.yaml": "edited, not committed\n", "untracked.txt": "u\n"})
	fixtureGit(t, r.dir, "add", "web/compose.yaml")
	writeTree(t, r.dir, map[string]string{"web/compose.yaml": "edited again\n"})
	head, _ := r.Head(ctx)
	status := fixtureGit(t, r.dir, "status", "--porcelain", "--untracked-files=all")
	if err := r.AddRemote(ctx, remote.bare); err != nil {
		t.Fatal(err)
	}
	if err := r.FetchBranch(ctx, "main"); err != nil {
		t.Fatal(err)
	}

	if _, err := r.CommitHostTree(ctx, "HEAD"); err == nil {
		t.Fatal("accepted a name for a commit id")
	}
	before := snapshot(t, r.dir)
	adopted, err := r.CommitHostTree(ctx, remoteHead)
	if err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", adopted+"^"); got != remoteHead {
		t.Fatalf("parent = %s", got)
	}
	if got, want := fixtureGit(t, r.dir, "rev-parse", adopted+"^{tree}"), fixtureGit(t, r.dir, "rev-parse", "HEAD^{tree}"); got != want {
		t.Fatalf("tree = %s, want %s", got, want)
	}
	if got := fixtureGit(t, r.dir, "show", "-s", "--format=%an|%ae|%P|%s", adopted); got != commitName+"|"+commitEmail+"|"+remoteHead+"|"+adoptMessage {
		t.Fatalf("commit = %q", got)
	}
	if h, _ := r.Head(ctx); h != head {
		t.Fatal("CommitHostTree moved HEAD")
	}
	if got := fixtureGit(t, r.dir, "status", "--porcelain", "--untracked-files=all"); got != status {
		t.Fatalf("status = %q, want %q", got, status)
	}
	// Only the objects are new.
	after := snapshot(t, r.dir)
	for p, b := range before {
		if rel, _ := filepath.Rel(r.dir, p); !strings.HasPrefix(rel, ".git") {
			if a := after[p]; a.mode != b.mode || !a.mtime.Equal(b.mtime) || a.body != b.body {
				t.Errorf("%s changed", rel)
			}
		}
	}

	// The branch moves only when the history it leaves is safe and the files stay the same.
	if err := r.ResetBranch(ctx, adopted); !errors.Is(err, ErrHistoryNotSaved) {
		t.Fatalf("unsaved history: %v", err)
	}
	if err := r.SaveLocalHistory(ctx, head); err != nil {
		t.Fatal(err)
	}
	if err := r.ResetBranch(ctx, remoteHead); !errors.Is(err, ErrTreeMismatch) {
		t.Fatalf("other tree: %v", err)
	}
	if err := r.ResetBranch(ctx, "refs/heads/main"); err == nil {
		t.Fatal("accepted a name for a commit id")
	}
	if got := fixtureGit(t, r.dir, "rev-parse", "main"); got != head {
		t.Fatalf("a refused reset moved the branch to %s", got)
	}
	before = snapshot(t, r.dir)
	if err := r.ResetBranch(ctx, adopted); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", "main"); got != adopted {
		t.Fatalf("branch = %s", got)
	}
	if got := fixtureGit(t, r.dir, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Fatalf("HEAD = %s", got)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
		t.Fatalf("upstream = %q", got)
	}
	if got := fixtureGit(t, r.dir, "status", "--porcelain", "--untracked-files=all"); got != status {
		t.Fatalf("status = %q, want %q", got, status)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", LocalHistoryRef); got != head {
		t.Fatalf("saved history = %s", got)
	}
	after = snapshot(t, r.dir)
	for p, b := range before {
		if rel, _ := filepath.Rel(r.dir, p); !strings.HasPrefix(rel, ".git") {
			if a := after[p]; a.mode != b.mode || !a.mtime.Equal(b.mtime) || a.body != b.body {
				t.Errorf("%s changed", rel)
			}
		}
	}

	// The same tree needs no new commit.
	if _, err := r.CommitHostTree(ctx, adopted); !errors.Is(err, ErrUnchanged) {
		t.Fatalf("same tree: %v", err)
	}

	// Now the push is a plain fast-forward on top of the remote history.
	if err := r.PushBranch(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, remote.bare, "rev-parse", "main"); got != adopted {
		t.Fatalf("remote main = %s", got)
	}
	if got := fixtureGit(t, remote.bare, "merge-base", "--is-ancestor", remoteHead, "main"); got != "" {
		t.Fatalf("remote history lost: %q", got)
	}
	if got, err := r.CompareRemote(ctx, remote.bare); err != nil || got.State != RemoteEqual || got.Commit != adopted {
		t.Fatalf("after: %+v, %v", got, err)
	}
	wantNoTempLeft(t)
}

func TestAdoptARemoteWithTheSameFilesMovesTheBranchToItsCommit(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t)
	remoteHead := fixtureGit(t, remote.work, "rev-parse", "HEAD")
	r := hostRepo(t, remoteFiles)
	fixtureGit(t, r.dir, "rm", "-q", "first.txt")
	fixtureGit(t, r.dir, "commit", "-q", "-m", "drop")
	head, _ := r.Head(ctx)
	if err := r.AddRemote(ctx, remote.bare); err != nil {
		t.Fatal(err)
	}
	if err := r.FetchBranch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitHostTree(ctx, remoteHead); !errors.Is(err, ErrUnchanged) {
		t.Fatalf("CommitHostTree: %v", err)
	}
	if err := r.SaveLocalHistory(ctx, head); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, r.dir)
	if err := r.ResetBranch(ctx, remoteHead); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", "main", LocalHistoryRef); got != remoteHead+"\n"+head {
		t.Fatalf("refs = %q", got)
	}
	wantClean(t, r)
	after := snapshot(t, r.dir)
	for p, b := range before {
		if rel, _ := filepath.Rel(r.dir, p); !strings.HasPrefix(rel, ".git") && (!after[p].mtime.Equal(b.mtime) || after[p].body != b.body) {
			t.Errorf("%s changed", rel)
		}
	}
	// Nothing is left to send; the push does not move the remote.
	if err := r.PushBranch(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, remote.bare, "rev-parse", "main"); got != remoteHead {
		t.Fatalf("remote main = %s", got)
	}
}

func TestPushBranchRefusesSecretsAndCreatesTheBranchOnAnEmptyRemote(t *testing.T) {
	ctx := context.Background()
	bare := emptyBare(t, "main")
	r := hostRepo(t, map[string]string{"web/compose.yaml": "a\n"})
	localCommit(t, r.dir, map[string]string{"web/secrets.env": "DEMO_PASSWORD=not-a-real-secret\n"})
	fixtureGit(t, r.dir, "rm", "-q", "web/secrets.env")
	fixtureGit(t, r.dir, "commit", "-q", "-m", "drop the secrets")
	if err := r.AddRemote(ctx, bare); err != nil {
		t.Fatal(err)
	}
	if err := r.SetUpstream(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r.dir, "config", "branch.main.merge"); got != "refs/heads/main" {
		t.Fatalf("merge = %q", got)
	}

	err := r.PushBranch(ctx)
	var secrets *SecretsError
	if !errors.As(err, &secrets) {
		t.Fatalf("err = %v, want a *SecretsError", err)
	}
	if secrets.Scan.Total != 1 || secrets.Scan.Findings[0].Path != "web/secrets.env" || secrets.Scan.Findings[0].Kind != KindSecretsFile {
		t.Fatalf("scan = %+v", secrets.Scan)
	}
	if strings.Contains(err.Error(), "not-a-real-secret") {
		t.Fatalf("the error holds a value: %v", err)
	}
	if got := fixtureGit(t, bare, "for-each-ref"); got != "" {
		t.Fatalf("the refused push created refs: %q", got)
	}
	if err := r.PushBranch(ctx); err == nil {
		t.Fatal("the second attempt went through")
	}
	if got := fixtureGit(t, bare, "for-each-ref"); got != "" {
		t.Fatalf("the refused push created refs: %q", got)
	}

	// The same history in a clean form goes through, creating the branch.
	clean := hostRepo(t, map[string]string{"web/compose.yaml": "a\n"})
	if err := clean.AddRemote(ctx, bare); err != nil {
		t.Fatal(err)
	}
	if err := clean.PushBranch(ctx); err != nil {
		t.Fatal(err)
	}
	head, _ := clean.Head(ctx)
	if got := fixtureGit(t, bare, "rev-parse", "main"); got != head {
		t.Fatalf("remote main = %s, want %s", got, head)
	}
	// A moved remote is a rejection, never a forced update.
	other := hostRepo(t, map[string]string{"web/compose.yaml": "b\n"})
	if err := other.AddRemote(ctx, bare); err != nil {
		t.Fatal(err)
	}
	if err := other.PushBranch(ctx); err == nil {
		t.Fatal("pushed a history that does not contain the remote branch")
	}
	if got := fixtureGit(t, bare, "rev-parse", "main"); got != head {
		t.Fatalf("remote main moved to %s", got)
	}
}

func TestInitFromRemoteStartsTheRepositoryOnTheRemoteHistory(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t)
	remoteHead := fixtureGit(t, remote.work, "rev-parse", "HEAD")
	cfg := testConfig(t)
	folder := func(files map[string]string) string {
		dir := filepath.Join(t.TempDir(), "stacks")
		writeTree(t, dir, files)
		old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
		for p := range treeState(t, dir) {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	check := func(dir string, before map[string]fileState) {
		t.Helper()
		after := treeState(t, dir)
		for p, b := range before {
			if a := after[p]; p != dir && (a.mode != b.mode || !a.mtime.Equal(b.mtime) || a.body != b.body) {
				t.Errorf("%s changed", p)
			}
		}
		wantClean(t, &Repo{dir: dir, cfg: cfg})
		if got := fixtureGit(t, dir, "symbolic-ref", "--short", "HEAD"); got != "main" {
			t.Errorf("branch = %q", got)
		}
		if got := fixtureGit(t, dir, "rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
			t.Errorf("upstream = %q", got)
		}
		if got := fixtureGit(t, dir, "rev-parse", "refs/remotes/origin/main"); got != remoteHead {
			t.Errorf("tracking ref = %s", got)
		}
		if got := fixtureGit(t, dir, "config", "--local", "core.sharedRepository"); got != "group" {
			t.Errorf("core.sharedRepository = %q", got)
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git", "info", "attributes")); !os.IsNotExist(err) {
			t.Errorf("attributes left behind: %v", err)
		}
	}

	// Equal: the branch is the remote commit.
	dir := folder(map[string]string{"README.md": "x\n", "web/compose.yaml": "a\n", "db/compose.yaml": "a\n", "web/secrets.env": "DEMO_TOKEN=not-a-real-secret\n"})
	before := treeState(t, dir)
	files := []string{"README.md", "db/compose.yaml", "web/compose.yaml"}
	id, err := InitFromRemote(ctx, cfg, dir, remote.bare, "main", remoteHead, files, []string{"web/secrets.env"})
	if err != nil || id != remoteHead {
		t.Fatalf("equal: %s, %v", id, err)
	}
	check(dir, before)
	if got := fixtureGit(t, dir, "rev-parse", "main"); got != remoteHead {
		t.Fatalf("main = %s", got)
	}
	if got := readFile(t, filepath.Join(dir, ".git", "info", "exclude")); got != "/web/secrets.env\n" {
		t.Fatalf("exclude = %q", got)
	}

	// Different: a new commit with the remote commit as its parent and exactly the given files.
	dir = folder(map[string]string{"README.md": "x\n", "web/compose.yaml": "mine\n", "new/compose.yaml": "n\n"})
	before = treeState(t, dir)
	id, err = InitFromRemote(ctx, cfg, dir, remote.bare, "main", remoteHead, []string{"README.md", "new/compose.yaml", "web/compose.yaml"}, nil)
	if err != nil || id == remoteHead {
		t.Fatalf("different: %s, %v", id, err)
	}
	check(dir, before)
	if got := fixtureGit(t, dir, "rev-parse", "main", "main^"); got != id+"\n"+remoteHead {
		t.Fatalf("refs = %q", got)
	}
	if got := fixtureGit(t, dir, "ls-tree", "-r", "--name-only", "main"); got != "README.md\nnew/compose.yaml\nweb/compose.yaml" {
		t.Fatalf("tree = %q", got)
	}
	if got := fixtureGit(t, remote.bare, "rev-parse", "main"); got != remoteHead {
		t.Fatalf("the remote moved: %s", got)
	}

	// The remote moved since the comparison, or a step fails: no .git is left.
	dir = folder(map[string]string{"README.md": "x\n"})
	before = treeState(t, dir)
	stale := strings.Repeat("0", 40)
	for name, c := range map[string]struct {
		url, commit string
		files       []string
	}{
		"moved":    {remote.bare, stale, []string{"README.md"}},
		"vanished": {remote.bare, remoteHead, []string{"README.md", "gone.txt"}},
		"url":      {"-x", remoteHead, []string{"README.md"}},
		"none":     {remote.bare, remoteHead, nil},
	} {
		if _, err := InitFromRemote(ctx, cfg, dir, c.url, "main", c.commit, c.files, nil); err == nil {
			t.Errorf("%s: no error", name)
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
			t.Errorf("%s: .git left behind: %v", name, err)
		}
	}
	if _, err := InitFromRemote(ctx, cfg, dir, remote.bare, "main", stale, []string{"README.md"}, nil); !errors.Is(err, ErrRemoteMoved) {
		t.Fatalf("moved: %v", err)
	}
	if _, err := InitFromRemote(ctx, cfg, dir, remote.bare, "--x", remoteHead, []string{"README.md"}, nil); err == nil {
		t.Fatal("accepted an option as a branch")
	}
	if after := treeState(t, dir); len(after) != len(before) {
		t.Fatalf("entries before %d, after %d", len(before), len(after))
	}
	if _, err := InitFromRemote(ctx, Config{Local: true}, dir, remote.bare, "main", remoteHead, []string{"README.md"}, nil); err == nil {
		t.Fatal("started in local mode")
	}

	// An empty remote: a first commit on the named branch, and the upstream is set for the first push.
	bare := emptyBare(t, "main")
	dir = folder(map[string]string{"README.md": "x\n"})
	id, err = InitFromRemote(ctx, cfg, dir, bare, "main", "", []string{"README.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, dir, "rev-parse", "main"); got != id {
		t.Fatalf("main = %s", got)
	}
	if got := fixtureGit(t, dir, "config", "branch.main.remote"); got != "origin" {
		t.Fatalf("remote = %q", got)
	}
	r, _ := Open(cfg, dir)
	if err := r.PushBranch(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, bare, "rev-parse", "main"); got != id {
		t.Fatalf("remote main = %s", got)
	}
}
