package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (f *remoteFixture) remove(path string) string {
	f.t.Helper()
	fixtureGit(f.t, f.work, "rm", "-q", "-r", "--", path)
	fixtureGit(f.t, f.work, "commit", "-q", "-m", "remove")
	fixtureGit(f.t, f.work, "push", "-q", f.bare, "main")
	return fixtureGit(f.t, f.work, "rev-parse", "HEAD")
}

func indexAndFiles(t *testing.T, r *Repo) (string, string) {
	t.Helper()
	idx, err := os.ReadFile(filepath.Join(r.dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(r.dir, "web", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(idx), string(body)
}

func TestRevertStackBuildsScopedCommitWithoutTouchingCopy(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	r, _ := cloneFixture(t, f)
	deployed, _ := r.Head(ctx)
	failed := f.commit(map[string]string{"web/compose.yaml": "bad\n", "web/new.env": "1\n"})
	other := f.commit(map[string]string{"db/compose.yaml": "other\n"})
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	idxBefore, bodyBefore := indexAndFiles(t, r)

	id, err := r.RevertStack(ctx, "web", deployed, failed, "health check\nfailed \x1b[31m")
	if err != nil {
		t.Fatal(err)
	}
	idxAfter, bodyAfter := indexAndFiles(t, r)
	if idxBefore != idxAfter || bodyBefore != bodyAfter {
		t.Fatal("RevertStack touched the index or the working copy")
	}
	if head, _ := r.Head(ctx); head != deployed {
		t.Fatal("RevertStack moved HEAD")
	}
	if got := fixtureGit(t, r.dir, "rev-parse", id+"^"); got != other {
		t.Fatalf("parent = %s, want remote head %s", got, other)
	}
	if got := fixtureGit(t, r.dir, "diff", "--name-status", other, id); got != "M\tweb/compose.yaml\nD\tweb/new.env" {
		t.Fatalf("diff = %q", got)
	}
	if got := fixtureGit(t, r.dir, "show", "-s", "--format=%an <%ae>|%cn <%ce>", id); got != "qdeploy <qdeploy@localhost.invalid>|qdeploy <qdeploy@localhost.invalid>" {
		t.Fatalf("identity = %q", got)
	}
	msg := fixtureGit(t, r.dir, "show", "-s", "--format=%B", id)
	if !strings.HasPrefix(msg, "Revert web to "+deployed[:12]+"\n") || !strings.Contains(msg, "- "+failed[:12]) ||
		strings.Contains(msg, other[:12]) || !strings.Contains(msg, "Reason: health check failed [31m") {
		t.Fatalf("message = %q", msg)
	}
	if strings.ContainsRune(msg, 0x1b) {
		t.Fatal("control character in message")
	}
	if got := fixtureGit(t, r.dir, "status", "--porcelain"); got != "" {
		t.Fatalf("status = %q", got)
	}
}

func TestRevertStackRestoresRemovedAndRemovesAddedStack(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	r, _ := cloneFixture(t, f)
	deployed, _ := r.Head(ctx)
	failed := f.remove("web")
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := r.RevertStack(ctx, "web", deployed, failed, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r.dir, "show", id+":web/compose.yaml"); got != "a" {
		t.Fatalf("restored content = %q", got)
	}

	// Absent at the deployed commit: the stack is removed again.
	g := newRemote(t)
	r2, _ := cloneFixture(t, g)
	d2, _ := r2.Head(ctx)
	n2 := g.commit(map[string]string{"cache/compose.yaml": "c\n"})
	if err := r2.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	id2, err := r2.RevertStack(ctx, "cache", d2, n2, "x")
	if err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, r2.dir, "ls-tree", "-r", "--name-only", id2); strings.Contains(got, "cache") {
		t.Fatalf("stack still present: %q", got)
	}
}

func TestRevertStackConflictAndNothingToRevert(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	r, _ := cloneFixture(t, f)
	deployed, _ := r.Head(ctx)
	failed := f.commit(map[string]string{"web/compose.yaml": "bad\n"})
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	// Other stack changes do not conflict, a later change to the stack does.
	f.commit(map[string]string{"db/compose.yaml": "z\n"})
	f.commit(map[string]string{"web/compose.yaml": "later\n"})
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RevertStack(ctx, "web", deployed, failed, ""); !errors.Is(err, ErrRevertConflict) {
		t.Fatalf("err = %v, want ErrRevertConflict", err)
	}
	// db did not change between deployed and failed: nothing to revert.
	if _, err := r.RevertStack(ctx, "db", deployed, deployed, ""); err == nil || !errors.Is(err, ErrRevertConflict) {
		// db differs at base from deployed, so this is a conflict by rule.
		t.Fatalf("err = %v", err)
	}
	g := newRemote(t)
	r2, _ := cloneFixture(t, g)
	d2, _ := r2.Head(ctx)
	n2 := g.commit(map[string]string{"db/compose.yaml": "z\n"})
	if err := r2.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r2.RevertStack(ctx, "web", d2, n2, ""); !errors.Is(err, ErrNothingToRevert) {
		t.Fatalf("err = %v, want ErrNothingToRevert", err)
	}
}

func TestRevertStackRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	r, _ := cloneFixture(t, f)
	head, _ := r.Head(ctx)
	for _, stack := range []string{"", "a/b", "../x", "-x", ".git", "*", ".hidden", "a b"} {
		if _, err := r.RevertStack(ctx, stack, head, head, ""); err == nil {
			t.Errorf("accepted stack %q", stack)
		}
	}
	if _, err := r.RevertStack(ctx, "web", "HEAD", head, ""); err == nil {
		t.Error("accepted a non-id commit")
	}
	if _, err := r.RevertStack(ctx, "web", strings.Repeat("0", 40), head, ""); err == nil {
		t.Error("accepted an unknown commit")
	}
}

func TestPushFastForwardsRemoteAndTrackingRef(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	r, _ := cloneFixture(t, f)
	deployed, _ := r.Head(ctx)
	failed := f.commit(map[string]string{"web/compose.yaml": "bad\n"})
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := r.RevertStack(ctx, "web", deployed, failed, "r")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, f.bare, "rev-parse", "main"); got != id {
		t.Fatalf("remote main = %s, want %s", got, id)
	}
	if rh, _ := r.RemoteHead(ctx); rh != id {
		t.Fatalf("tracking ref = %s", rh)
	}
	if err := r.FastForward(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.dir, "web", "compose.yaml")); string(got) != "a\n" {
		t.Fatalf("working copy = %q", got)
	}
}

func TestPushRejectedWhenRemoteMoved(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	r, _ := cloneFixture(t, f)
	deployed, _ := r.Head(ctx)
	failed := f.commit(map[string]string{"web/compose.yaml": "bad\n"})
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := r.RevertStack(ctx, "web", deployed, failed, "")
	if err != nil {
		t.Fatal(err)
	}

	moved := f.commit(map[string]string{"db/compose.yaml": "new\n"}) // unknown to the copy: "fetch first"
	if err := r.Push(ctx, id); !errors.Is(err, ErrPushRejected) {
		t.Fatalf("err = %v, want ErrPushRejected", err)
	}
	if err := r.Fetch(ctx); err != nil { // known to the copy: "non-fast-forward"
		t.Fatal(err)
	}
	if err := r.Push(ctx, id); !errors.Is(err, ErrPushRejected) {
		t.Fatalf("err = %v, want ErrPushRejected", err)
	}
	if got := fixtureGit(t, f.bare, "rev-parse", "main"); got != moved {
		t.Fatal("rejected push changed the remote")
	}
	// Rebuilt on the new head the push goes through.
	id, err = r.RevertStack(ctx, "web", deployed, failed, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Push(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func TestPushOtherFailuresAreNotRejections(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	r, _ := cloneFixture(t, f)
	head, _ := r.Head(ctx)
	if err := r.Push(ctx, "main"); err == nil {
		t.Fatal("accepted a non-id")
	}
	fixtureGit(t, r.dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	err := r.Push(ctx, head)
	if err == nil || errors.Is(err, ErrPushRejected) {
		t.Fatalf("err = %v", err)
	}
}

func TestIsNonFastForward(t *testing.T) {
	cases := map[string]bool{
		"To x\n!\tabc:refs/heads/main\t[rejected] (non-fast-forward)\nDone\n": true,
		"!\tabc:refs/heads/main\t[rejected] (fetch first)\n":                  true,
		"!\tabc:refs/heads/main\t[remote rejected] (pre-receive hook)\n":      false,
		"*\tabc:refs/heads/main\t[new branch]\n":                              false,
		"":                                                                    false,
	}
	for in, want := range cases {
		if got := isNonFastForward(in); got != want {
			t.Errorf("%q = %v", in, got)
		}
	}
}

// Forced updates would destroy remote history a rollback must never rewrite.
func TestNoForcePushInCode(t *testing.T) {
	banned := []string{`"--` + `force"`, "force-if" + "-includes", "force-with" + "-lease", `"-` + `f"`, `"+` + `refs`, `"+` + `HEAD`, `:+` + `refs`}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || filepath.Base(p) == "revert_test.go" {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body), `"push"`) {
			return nil // only code that runs git push is of interest
		}
		for _, b := range banned {
			if strings.Contains(string(body), b) {
				t.Errorf("%s contains %s", p, b)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
