package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// layoutRemote is a remote whose stack web lies below prefix: "web/" for a subfolder, "" for a root stack.
// Both forms carry the settings of web in .qatlas.
func layoutRemote(t *testing.T, prefix string) *remoteFixture {
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
	f.commit(map[string]string{prefix + "compose.yaml": "a\n", prefix + "sub/compose.yaml": "a\n", ".qatlas/qdeploy/web.yaml": "s1\n"})
	return f
}

var forms = []struct{ stack, prefix string }{{"web", "web/"}, {RootStack, ""}}

func TestChangedFoldersOfARootStack(t *testing.T) {
	ctx := context.Background()
	f := layoutRemote(t, "")
	r, _ := cloneFixture(t, f)
	head, _ := r.Head(ctx)
	settings := f.commit(map[string]string{".qatlas/qdeploy/web.yaml": "s2\n"})
	top := f.commit(map[string]string{"compose.yaml": "b\n"})
	sub := f.commit(map[string]string{"sub/compose.yaml": "b\n"})
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		from, to string
		want     []string
	}{{head, settings, []string{".qatlas"}}, {settings, top, []string{RootStack}}, {top, sub, []string{RootStack, "sub"}}} {
		if got, err := r.ChangedFolders(ctx, tc.from, tc.to); err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("ChangedFolders(%s..%s) = %v, %v; want %v", tc.from[:7], tc.to[:7], got, err, tc.want)
		}
	}
}

func TestTrackedFilesOfARootStack(t *testing.T) {
	f := layoutRemote(t, "")
	f.commit(map[string]string{"secrets.env": "DEMO_PASSWORD=not-a-real-secret\n", "data/db.bin": "x\n"})
	r, _ := cloneFixture(t, f)
	got, err := r.TrackedFiles(context.Background(), RootStack)
	want := []string{".qatlas/qdeploy/web.yaml", "compose.yaml", "data/db.bin", "secrets.env", "sub/compose.yaml"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("TrackedFiles = %v, %v; want %v", got, err, want)
	}
}

// A revert gives a root stack the same commit as the stack in a subfolder: the stack goes back, its settings
// stay as on the branch.
func TestRevertStackOfARootStackLikeASubfolder(t *testing.T) {
	ctx := context.Background()
	for _, form := range forms {
		t.Run(form.stack, func(t *testing.T) {
			f := layoutRemote(t, form.prefix)
			r, _ := cloneFixture(t, f)
			deployed, _ := r.Head(ctx)
			failed := f.commit(map[string]string{form.prefix + "compose.yaml": "bad\n", form.prefix + "sub/new.env": "1\n",
				".qatlas/qdeploy/web.yaml": "s2\n"})
			settings := f.commit(map[string]string{".qatlas/qdeploy/web.yaml": "s3\n"})
			if err := r.Fetch(ctx); err != nil {
				t.Fatal(err)
			}
			id, err := r.RevertStack(ctx, form.stack, deployed, failed, "check failed")
			if err != nil {
				t.Fatal(err)
			}
			if got := fixtureGit(t, r.dir, "rev-parse", id+"^"); got != settings {
				t.Fatalf("parent = %s, want %s", got, settings)
			}
			want := "M\t" + form.prefix + "compose.yaml\nD\t" + form.prefix + "sub/new.env"
			if got := fixtureGit(t, r.dir, "diff", "--name-status", settings, id); got != want {
				t.Fatalf("diff = %q, want %q", got, want)
			}
			msg := fixtureGit(t, r.dir, "show", "-s", "--format=%B", id)
			if !strings.Contains(msg, "- "+failed[:12]) || strings.Contains(msg, settings[:12]) {
				t.Fatalf("message = %q", msg)
			}

			// A later change of the stack conflicts; one of the settings alone does not.
			f.commit(map[string]string{form.prefix + "compose.yaml": "later\n"})
			if err := r.Fetch(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := r.RevertStack(ctx, form.stack, deployed, failed, ""); !errors.Is(err, ErrRevertConflict) {
				t.Fatalf("err = %v, want ErrRevertConflict", err)
			}
		})
	}
}

func TestRevertStackOfARootStackWithNothingToRevert(t *testing.T) {
	ctx := context.Background()
	for _, form := range forms {
		f := layoutRemote(t, form.prefix)
		r, _ := cloneFixture(t, f)
		deployed, _ := r.Head(ctx)
		failed := f.commit(map[string]string{".qatlas/qdeploy/web.yaml": "s2\n"})
		if err := r.Fetch(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := r.RevertStack(ctx, form.stack, deployed, failed, ""); !errors.Is(err, ErrNothingToRevert) {
			t.Errorf("%s: err = %v, want ErrNothingToRevert", form.stack, err)
		}
	}
	f := layoutRemote(t, "")
	r, _ := cloneFixture(t, f)
	deployed, _ := r.Head(ctx)
	failed := f.commit(map[string]string{"compose.yaml": "bad\n"})
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := r.RevertStack(ctx, RootStack, deployed, failed, "")
	if err != nil {
		t.Fatal(err)
	}
	if msg := fixtureGit(t, r.dir, "show", "-s", "--format=%B", id); !strings.HasPrefix(msg, "Revert the stack folder to "+deployed[:12]+"\n") {
		t.Fatalf("message = %q", msg)
	}
}
