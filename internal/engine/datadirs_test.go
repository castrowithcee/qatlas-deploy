package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// dirsDeployer is the fake deployer with a data directory check: it reports dirs while their paths are
// missing below the stack folder.
type dirsDeployer struct {
	*fakeDeployer
	dirs      []MissingDir
	err       error
	checks    int
	versioned map[string]bool
}

func (d *dirsDeployer) MissingDataDirs(ctx context.Context, st stacks.Stack,
	versioned func(context.Context, string) (bool, error), _ *Log) ([]MissingDir, error) {
	d.checks++
	if d.err != nil {
		return nil, d.err
	}
	var out []MissingDir
	for _, m := range d.dirs {
		if _, err := os.Lstat(filepath.Join(st.Dir, m.Path)); err == nil {
			continue
		}
		v, err := versioned(ctx, m.Path)
		if err != nil {
			return nil, err
		}
		if d.versioned == nil {
			d.versioned = map[string]bool{}
		}
		d.versioned[m.Path] = v
		if !v {
			out = append(out, m)
		}
	}
	return out, nil
}

func dirsRig(t *testing.T, dirs ...MissingDir) (*rig, *dirsDeployer, func() []Event) {
	t.Helper()
	r := newRig(t, "web")
	d := &dirsDeployer{fakeDeployer: r.dep, dirs: dirs}
	r.cfg.Deployer = d
	events := r.withEvents()
	return r, d, events
}

func dirFinding(t *testing.T, st StackState) Finding {
	t.Helper()
	for _, f := range st.Findings {
		if f.Kind == FindingDataDirMissing {
			return f
		}
	}
	t.Fatalf("no data-dir-missing finding: %+v", st.Findings)
	return Finding{}
}

func TestMissingDataDirsBlockTheStartAndNotify(t *testing.T) {
	want := []MissingDir{{Path: "data", Owner: "1000:1000"}}
	r, d, events := dirsRig(t, want...)
	r.poll()

	if steps := r.dep.stepsOf("web"); len(steps) != 0 {
		t.Fatalf("the stack was touched: %v", steps)
	}
	st := r.stack("web")
	if st.LastRun == nil || st.LastRun.Result != ResultFailed || st.Deployed != "" || st.FailedCommit != "" {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
	if f := dirFinding(t, st); !slices.Equal(f.Dirs, want) || f.Detail == "" {
		t.Fatalf("finding = %+v", f)
	}
	if ev := events(); len(ev) != 1 || ev[0].Kind != ResultFailed || ev[0].Stack != "web" || ev[0].Error == "" {
		t.Fatalf("events = %+v", ev)
	}

	// Still waiting: the next polls start nothing, notify nothing and keep the finding.
	r.poll()
	r.poll()
	if d.checks != 1 || len(events()) != 1 || len(r.dep.stepsOf("web")) != 0 {
		t.Fatalf("checks=%d events=%d steps=%v", d.checks, len(events()), r.dep.stepsOf("web"))
	}
	if f := dirFinding(t, r.stack("web")); !slices.Equal(f.Dirs, want) {
		t.Fatalf("finding = %+v", f)
	}

	// The directory appears: the next poll runs the stack and the wait is gone.
	if err := os.Mkdir(filepath.Join(r.wc, "web", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.poll()
	st = r.stack("web")
	if st.Deployed != commit(1) || st.DataDirs != nil || hasFinding(st, FindingDataDirMissing) || st.LastRun.Result != ResultSucceeded {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
}

func TestDataDirWaitPersistsAcrossRestartAndEndsWithANewCommit(t *testing.T) {
	r, d, _ := dirsRig(t, MissingDir{Path: "data"})
	r.poll()
	r.eng = r.start()
	if f := dirFinding(t, r.stack("web")); len(f.Dirs) != 1 {
		t.Fatalf("finding = %+v", f)
	}
	r.poll()
	if d.checks != 1 {
		t.Fatalf("a restart repeated the run: %d", d.checks)
	}
	// A new commit runs and checks again.
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{"", commit(2)}] = []string{"web"}
	})
	r.poll()
	if d.checks != 2 {
		t.Fatalf("checks = %d", d.checks)
	}
}

func TestManualRunChecksTheDataDirs(t *testing.T) {
	r, d, _ := dirsRig(t, MissingDir{Path: "data"})
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	r.eng.Wait()
	if d.checks != 1 || len(r.dep.stepsOf("web")) != 0 || !hasFinding(r.stack("web"), FindingDataDirMissing) {
		t.Fatalf("checks=%d steps=%v", d.checks, r.dep.stepsOf("web"))
	}
}

func TestNoMissingDirsRunsAsBefore(t *testing.T) {
	r, d, _ := dirsRig(t)
	r.poll()
	if d.checks != 1 || r.stack("web").Deployed != commit(1) || r.stack("web").DataDirs != nil {
		t.Fatalf("checks=%d web=%+v", d.checks, r.stack("web"))
	}
	if steps := r.dep.stepsOf("web"); !slices.Equal(steps, []string{"stop", "pull", "up", "check"}) {
		t.Fatalf("steps = %v", steps)
	}
}

func TestDataDirCheckErrorFailsTheRunBeforeTheStop(t *testing.T) {
	r, d, events := dirsRig(t)
	d.err = errors.New("config unreadable")
	r.poll()
	st := r.stack("web")
	if len(r.dep.stepsOf("web")) != 0 || st.LastRun.Result != ResultFailed || st.FailedCommit != commit(1) ||
		st.DataDirs != nil || len(events()) != 1 {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
}

type trackedCopy struct {
	*fakeCopy
	files []string
}

func (c trackedCopy) TrackedFiles(_ context.Context, dir string) ([]string, error) {
	if dir != "web" {
		return nil, errors.New("unexpected folder")
	}
	return c.files, nil
}

func TestVersionedPathsAreNeverCandidates(t *testing.T) {
	r, d, _ := dirsRig(t, MissingDir{Path: "tracked"}, MissingDir{Path: "other"}, MissingDir{Path: "tr"})
	r.cfg.Source = func() (Binding, error) {
		return Binding{Dir: r.wc, Copy: trackedCopy{r.copy, []string{"web/tracked/keep", "web/trunk/x"}}}, nil
	}
	r.eng = r.start()
	r.poll()
	f := dirFinding(t, r.stack("web"))
	if !slices.Equal(f.Dirs, []MissingDir{{Path: "other"}, {Path: "tr"}}) || !d.versioned["tracked"] || d.versioned["tr"] {
		t.Fatalf("finding = %+v, versioned = %v", f, d.versioned)
	}
}

func TestDataDirStateIsValidated(t *testing.T) {
	ok := State{Schema: stateSchema, Stacks: map[string]StackState{"web": {DataDirs: &DataDirWait{
		Commit: commit(1), Dirs: []MissingDir{{Path: "a/b", Owner: "1:2"}}}}}}
	if err := validateState(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []MissingDir{{Path: "../x"}, {Path: "/abs"}, {Path: "a/../b"}, {Path: "."}, {Path: ""}, {Path: "a", Owner: "root"}} {
		s := ok
		s.Stacks = map[string]StackState{"web": {DataDirs: &DataDirWait{Commit: commit(1), Dirs: []MissingDir{bad}}}}
		if validateState(s) == nil {
			t.Fatalf("%+v accepted", bad)
		}
	}
}
