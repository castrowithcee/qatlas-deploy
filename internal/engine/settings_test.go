package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

func rulesOf(t *testing.T, text string) *stacks.Rules {
	t.Helper()
	r, err := stacks.ParseSettings([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

const automaticSettings = "version: 1\nupdate:\n  mode: automatic\n"

func TestSetSettingsCommitsPushesAndFollows(t *testing.T) {
	r := newRig(t, "web", "db")
	r.deployAll()
	ctx := context.Background()
	if got, err := r.eng.StackSettings(ctx, "web"); err != nil || got != nil {
		t.Fatalf("settings before = %+v, %v", got, err)
	}

	c, err := r.eng.SetSettings(ctx, "web", rulesOf(t, automaticSettings))
	if err != nil || c != commit(2001) {
		t.Fatalf("set = %q, %v", c, err)
	}
	want, _ := stacks.EncodeSettings(rulesOf(t, automaticSettings))
	if cs := r.copy.commits; len(cs) != 1 || cs[0].Path != ".qatlas/qdeploy/web.yaml" || cs[0].Message != "qdeploy: settings of web" ||
		cs[0].Old != nil || string(cs[0].New) != string(want) || cs[0].Base != commit(1) {
		t.Fatalf("commits = %+v", cs)
	}
	if !strings.HasPrefix(string(want), "version: 1\nupdate:\n  mode: automatic\n") {
		t.Fatalf("content = %q", want)
	}
	if !slices.Equal(r.copy.pushed, []string{c}) || !slices.Equal(r.copy.ffTo, []string{c}) {
		t.Fatalf("pushed %v, fast-forwarded to %v", r.copy.pushed, r.copy.ffTo)
	}
	if got, err := r.eng.StackSettings(ctx, "web"); err != nil || got == nil || got.Update.Mode != stacks.ModeAutomatic {
		t.Fatalf("settings after = %+v, %v", got, err)
	}

	// The same settings again change nothing.
	c, err = r.eng.SetSettings(ctx, "web", rulesOf(t, "version: 1\nupdate: {mode: automatic, ceiling: minor}\n"))
	if err != nil || c != "" || len(r.copy.commits) != 1 || len(r.copy.pushed) != 1 {
		t.Fatalf("unchanged set = %q, %v, commits %d", c, err, len(r.copy.commits))
	}
	if r.dep.got() != nil {
		t.Fatalf("a run was started: %v", r.dep.got())
	}
}

func TestSetSettingsRefusesWithoutCommit(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	ctx := context.Background()
	cases := map[string]struct {
		stack, settings string
		want            error
	}{
		"unknown variable": {"web", "version: 1\nupdate:\n  tags:\n    OTHER_VERSION: '.*'\n", ErrInvalidSettings},
		"missing compose":  {"web", "version: 1\ncompose:\n  files: [missing.yaml]\n", ErrInvalidSettings},
		"unknown stack":    {"nope", automaticSettings, ErrUnknownStack},
	}
	for name, c := range cases {
		if _, err := r.eng.SetSettings(ctx, c.stack, rulesOf(t, c.settings)); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	r.copy.set(func(f *fakeCopy) { f.changes = []gitrepo.Change{{Path: "README.md"}} })
	if _, err := r.eng.SetSettings(ctx, "web", rulesOf(t, automaticSettings)); !errors.Is(err, ErrCopyChanges) {
		t.Errorf("local changes: err = %v", err)
	}
	if n := r.copy.called("commit"); n != 0 || len(r.copy.pushed) != 0 {
		t.Fatalf("commits %d, pushes %v", n, r.copy.pushed)
	}
	if _, err := os.Stat(filepath.Join(r.wc, ".qatlas")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the working copy was written: %v", err)
	}
}

func TestSetSettingsRepairsBrokenSettings(t *testing.T) {
	r := newRig(t, "web")
	writeSettings(t, r.wc, "web", "version: 1\nupdate:\n  mode: sometimes\n")
	ctx := context.Background()
	if _, err := r.eng.StackSettings(ctx, "web"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("get: %v", err)
	}
	if _, err := r.eng.SetSettings(ctx, "web", rulesOf(t, automaticSettings)); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got, err := r.eng.StackSettings(ctx, "web"); err != nil || got.Update.Mode != stacks.ModeAutomatic {
		t.Fatalf("settings = %+v, %v", got, err)
	}
}

func TestSetSettingsFollowsTheBranchAndRetriesARejectedPush(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.pushErrs = []error{gitrepo.ErrPushRejected}
		f.onFetch = func(f *fakeCopy) {
			if len(f.commits) == 1 {
				f.remote = commit(3) // the branch moved after the first commit was built
			}
		}
	})
	c, err := r.eng.SetSettings(context.Background(), "web", rulesOf(t, automaticSettings))
	if err != nil || c != commit(2002) {
		t.Fatalf("set = %q, %v", c, err)
	}
	if cs := r.copy.commits; len(cs) != 2 || cs[0].Base != commit(2) || cs[1].Base != commit(3) {
		t.Fatalf("commits = %+v", cs)
	}
	if !slices.Equal(r.copy.ffTo, []string{commit(2), commit(3), commit(2002)}) {
		t.Fatalf("fast-forwards = %v", r.copy.ffTo)
	}
}

func TestSetSettingsWaitsForRunsAndAnswersBusy(t *testing.T) {
	r := newRig(t, "web")
	r.cfg.LockWait = 50 * time.Millisecond
	r.eng = r.start()
	r.deployAll()
	r.dep.block = make(chan struct{})
	r.dep.entered = make(chan struct{}, 4)
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	<-r.dep.entered
	if _, err := r.eng.SetSettings(context.Background(), "web", rulesOf(t, automaticSettings)); !errors.Is(err, ErrBusy) {
		t.Fatalf("set during a run: %v", err)
	}
	close(r.dep.block)
	r.eng.Wait()
	if n := r.copy.called("commit"); n != 0 {
		t.Fatalf("commits = %d", n)
	}
	if _, err := r.eng.SetSettings(context.Background(), "web", rulesOf(t, automaticSettings)); err != nil {
		t.Fatalf("set after the run: %v", err)
	}
}
