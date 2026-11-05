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
)

// localFixture is a repository without a remote, opened in local mode.
func localFixture(t *testing.T) *Repo {
	t.Helper()
	if _, err := lookIn(fixedPath, "git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := filepath.Join(t.TempDir(), "stacks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, dir, "init", "-q", "-b", "main")
	localCommit(t, dir, map[string]string{"README.md": "x\n", "web/.env": "APP_VERSION=1.0.0\n", "db/compose.yaml": "a\n"})
	r, err := Open(Config{Local: true}, dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// localCommit commits files in the working copy as a person would.
func localCommit(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	writeTree(t, dir, files)
	fixtureGit(t, dir, "add", "-A")
	fixtureGit(t, dir, "commit", "-q", "-m", "change")
	return fixtureGit(t, dir, "rev-parse", "HEAD")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantClean(t *testing.T, r *Repo) {
	t.Helper()
	if got := fixtureGit(t, r.dir, "status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Fatalf("status = %q", got)
	}
}

func TestLocalModeHasNoRemote(t *testing.T) {
	ctx := context.Background()
	r := localFixture(t)
	if err := r.Fetch(ctx); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	head, _ := r.Head(ctx)
	if base, err := r.RemoteHead(ctx); err != nil || base != head {
		t.Fatalf("RemoteHead = %q, %v; want %s", base, err, head)
	}
	if _, err := r.RemoteURL(ctx); err == nil {
		t.Fatal("a local working copy has a remote URL")
	}
	if _, err := Clone(ctx, Config{Local: true}, "git@git.example.test:demo/stacks.git", t.TempDir()); err == nil {
		t.Fatal("cloned in local mode")
	}
	if _, err := Open(Config{Local: true, KeyFile: "rel/key"}, "/x"); err == nil {
		t.Fatal("accepted an unsafe key path in local mode")
	}

	fixtureGit(t, r.dir, "checkout", "-q", "--detach")
	if err := r.Fetch(ctx); err == nil {
		t.Fatal("fetch on a detached HEAD succeeded")
	}
}

func TestLocalModeVersionChangeIsCommittedAndLands(t *testing.T) {
	ctx := context.Background()
	r := localFixture(t)
	base, _ := r.Head(ctx)
	c, err := r.CommitFile(ctx, "web/.env", setValue, "Update web APP_VERSION from 1.0.0 to 1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if head, _ := r.Head(ctx); head != base {
		t.Fatal("CommitFile moved HEAD")
	}
	if got := fixtureGit(t, r.dir, "rev-parse", c+"^"); got != base {
		t.Fatalf("parent = %s, want HEAD %s", got, base)
	}
	if err := r.Push(ctx, c); err != nil {
		t.Fatalf("land: %v", err)
	}
	if head, _ := r.Head(ctx); head != c {
		t.Fatalf("HEAD = %s, want %s", head, c)
	}
	if got := readFile(t, filepath.Join(r.dir, "web", ".env")); got != "APP_VERSION=1.1.0\n" {
		t.Fatalf(".env = %q", got)
	}
	wantClean(t, r)
	// the engine follows with a fast-forward, which has nothing left to do
	if err := r.FastForward(ctx, c); err != nil {
		t.Fatalf("fast-forward after landing: %v", err)
	}
	if got := fixtureGit(t, r.dir, "remote"); got != "" {
		t.Fatalf("remotes = %q", got)
	}
}

func TestLocalModeRevertLandsWithoutPush(t *testing.T) {
	ctx := context.Background()
	r := localFixture(t)
	deployed, _ := r.Head(ctx)
	failed := localCommit(t, r.dir, map[string]string{"web/.env": "APP_VERSION=2.0.0\n", "web/new.txt": "n\n"})
	other := localCommit(t, r.dir, map[string]string{"db/compose.yaml": "b\n"})

	c, err := r.RevertStack(ctx, "web", deployed, failed, "check failed")
	if err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", c+"^"); got != other {
		t.Fatalf("parent = %s, want HEAD %s", got, other)
	}
	if err := r.Push(ctx, c); err != nil {
		t.Fatalf("land: %v", err)
	}
	if head, _ := r.Head(ctx); head != c {
		t.Fatalf("HEAD = %s, want %s", head, c)
	}
	if got := readFile(t, filepath.Join(r.dir, "web", ".env")); got != "APP_VERSION=1.0.0\n" {
		t.Fatalf(".env = %q", got)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "web", "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new.txt still there: %v", err)
	}
	if got := readFile(t, filepath.Join(r.dir, "db", "compose.yaml")); got != "b\n" {
		t.Fatalf("db changed: %q", got)
	}
	wantClean(t, r)
}

func TestLocalModeLandingAfterAPersonCommittedIsRejectedThenRebuilt(t *testing.T) {
	ctx := context.Background()
	r := localFixture(t)
	first, err := r.CommitFile(ctx, "web/.env", setValue, "Update web")
	if err != nil {
		t.Fatal(err)
	}
	person := localCommit(t, r.dir, map[string]string{"db/compose.yaml": "person\n"})

	if err := r.Push(ctx, first); !errors.Is(err, ErrPushRejected) {
		t.Fatalf("land over a moved branch: %v", err)
	}
	if head, _ := r.Head(ctx); head != person {
		t.Fatalf("a rejected landing moved HEAD to %s", head)
	}
	wantClean(t, r)

	again, err := r.CommitFile(ctx, "web/.env", setValue, "Update web")
	if err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r.dir, "rev-parse", again+"^"); got != person {
		t.Fatalf("parent = %s, want %s", got, person)
	}
	if err := r.Push(ctx, again); err != nil {
		t.Fatalf("second landing: %v", err)
	}
	if head, _ := r.Head(ctx); head != again {
		t.Fatalf("HEAD = %s, want %s", head, again)
	}
	if got := readFile(t, filepath.Join(r.dir, "web", ".env")); got != "APP_VERSION=1.1.0\n" {
		t.Fatalf(".env = %q", got)
	}
	if got := readFile(t, filepath.Join(r.dir, "db", "compose.yaml")); got != "person\n" {
		t.Fatalf("the person's change is gone: %q", got)
	}
	wantClean(t, r)
}

func TestSafeDirectoryAdmitsAWorkingCopyOfAnotherOwner(t *testing.T) {
	ctx := context.Background()
	r := localFixture(t)
	if opts := r.baseOptions(); !slices.Contains(opts, "safe.directory="+r.dir) {
		t.Fatalf("base options lack safe.directory: %v", opts)
	}
	// git's own test switch treats the repository as owned by someone else
	foreign := "GIT_TEST_ASSUME_DIFFERENT_OWNER=1"
	plain := exec.Command("git", "-C", r.dir, "rev-parse", "HEAD")
	plain.Env = []string{"PATH=" + fixedPath, "HOME=" + t.TempDir(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", foreign}
	if out, err := plain.CombinedOutput(); err == nil || !strings.Contains(string(out), "dubious ownership") {
		t.Skipf("this git does not honour %s: %v %s", foreign, err, out)
	}
	if _, err := r.runEnv(ctx, true, maxErrorText, []string{foreign}, "rev-parse", "HEAD"); err != nil {
		t.Fatalf("git refused the working copy of another owner: %v", err)
	}
}
