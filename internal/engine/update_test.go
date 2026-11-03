package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const demoEnv = "# demo stack\nAPP_VERSION=1.0.0 # pinned\nOTHER=kept\n"

// updateRig deploys the stack web at commit 1 with demoEnv as its .env.
func updateRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, "web")
	if err := os.WriteFile(filepath.Join(r.wc, "web", ".env"), []byte(demoEnv), 0o644); err != nil {
		t.Fatal(err)
	}
	r.deployAll()
	return r
}

func step(from, to string) VersionStep {
	return VersionStep{Variable: "APP_VERSION", From: from, To: to}
}

func (r *rig) update(steps ...VersionStep) []StepResult {
	r.t.Helper()
	res, err := r.eng.Update(context.Background(), "web", steps)
	if err != nil {
		r.t.Fatalf("update: %v", err)
	}
	return res
}

func (r *rig) env() string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.wc, "web", ".env"))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func envWith(v string) string { return strings.Replace(demoEnv, "1.0.0", v, 1) }

func TestUpdateCommitsPushesAndDeploysOneStep(t *testing.T) {
	r := updateRig(t)
	events := r.withEvents()
	var pulled []string
	r.dep.hook = func(_ context.Context, step string, st stacks.Stack) error {
		if step == "pull" {
			pulled = append(pulled, st.Images[0].Value)
		}
		return nil
	}
	res := r.update(step("1.0.0", "1.1.0"))
	if len(res) != 1 || res[0].Result != ResultSucceeded || res[0].Commit != commit(2001) || res[0].Run == "" || res[0].Error != "" {
		t.Fatalf("results = %+v", res)
	}
	wantSteps(t, r, "stop", "backup", "pull", "up", "check")
	if c := r.copy.commits; len(c) != 1 || c[0].Path != "web/.env" || c[0].Base != commit(1) ||
		c[0].Message != "Update web APP_VERSION from 1.0.0 to 1.1.0" || string(c[0].New) != envWith("1.1.0") {
		t.Fatalf("commits = %+v", c)
	}
	if !slices.Equal(r.copy.pushed, []string{commit(2001)}) || r.env() != envWith("1.1.0") || !slices.Equal(pulled, []string{"1.1.0"}) {
		t.Fatalf("pushed %v, env %q, pulled %v", r.copy.pushed, r.env(), pulled)
	}
	if got := r.dep.got(); len(got) != 1 || got[0].Commit != commit(2001) {
		t.Fatalf("pull ran at %v", got)
	}
	st := r.stack("web")
	if st.Deployed != commit(2001) || st.FailedCommit != "" || st.LastRun.Trigger != TriggerUpdate || st.LastRun.Commit != commit(2001) ||
		st.LastRun.Backup != backupID || !slices.Equal(r.dep.backedUp, []string{commit(1)}) {
		t.Fatalf("web = %+v / %+v / backups %v", st, st.LastRun, r.dep.backedUp)
	}
	text := r.runLog(st)
	for _, want := range []string{"(update APP_VERSION 1.0.0 -> 1.1.0)", "step commit", "commit " + commit(2001), "step push",
		"step fast-forward", "the working copy is at " + commit(2001), "result succeeded"} {
		if !strings.Contains(text, want) {
			t.Fatalf("log lacks %q:\n%s", want, text)
		}
	}
	if ev := events(); len(ev) != 1 || ev[0].Kind != ResultSucceeded || ev[0].Commit != commit(2001) {
		t.Fatalf("events = %+v", ev)
	}
	if snap, _ := r.eng.Snapshot(); len(snap.Active) != 0 {
		t.Fatalf("still active: %v", snap.Active)
	}

	// the pushed commit is the deployed state: polls start nothing
	r.copy.set(func(f *fakeCopy) { f.folders[[2]string{commit(1), commit(2001)}] = []string{"web"} })
	if res := r.poll(); len(res.Started) != 0 || len(res.Dropped) != 0 || len(r.dep.got()) != 1 {
		t.Fatalf("poll after the update: %+v", res)
	}
}

func TestUpdateStepwiseRunsEachStepWithItsOwnBackupAndCommit(t *testing.T) {
	r := updateRig(t)
	res := r.update(step("1.0.0", "1.2.0"), step("1.2.0", "2.3.0"), step("2.3.0", "3.1.0"))
	for i, s := range res {
		if s.Result != ResultSucceeded || s.Commit != commit(2001+i) {
			t.Fatalf("step %d = %+v", i, s)
		}
	}
	if !slices.Equal(r.dep.backedUp, []string{commit(1), commit(2001), commit(2002)}) {
		t.Fatalf("backups of %v", r.dep.backedUp)
	}
	want := []string{"Update web APP_VERSION from 1.0.0 to 1.2.0", "Update web APP_VERSION from 1.2.0 to 2.3.0", "Update web APP_VERSION from 2.3.0 to 3.1.0"}
	for i, c := range r.copy.commits {
		if c.Message != want[i] || string(c.Old) != envWith(strings.Fields(want[i])[4]) || string(c.New) != envWith(strings.Fields(want[i])[6]) {
			t.Fatalf("commit %d = %+v", i, c)
		}
	}
	if len(r.copy.commits) != 3 || r.env() != envWith("3.1.0") || r.stack("web").Deployed != commit(2003) {
		t.Fatalf("commits %d, env %q, deployed %s", len(r.copy.commits), r.env(), r.stack("web").Deployed)
	}
	if runs, _ := r.eng.Runs("web"); len(runs) != 4 || runs[0].Trigger != TriggerUpdate || runs[2].Trigger != TriggerUpdate {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestUpdateFailureInTheMiddleStepRevertsOnlyThatStep(t *testing.T) {
	r := updateRig(t)
	r.dep.fail("check", nil, errBoom)
	res := r.update(step("1.0.0", "1.2.0"), step("1.2.0", "2.3.0"), step("2.3.0", "3.1.0"))
	if res[0].Result != ResultSucceeded || res[1].Result != ResultRolledBack || !strings.Contains(res[1].Error, "boom") ||
		res[1].Commit != commit(2002) || res[2].Run != "" || res[2].Result != "" || res[2].To != "3.1.0" {
		t.Fatalf("results = %+v", res)
	}
	wantSteps(t, r, "stop", "backup", "pull", "up", "check",
		"stop", "backup", "pull", "up", "check", "stop", "restore "+backupID, "up", "check")
	if rv := r.copy.reverts; len(rv) != 1 || rv[0].Deployed != commit(2001) || rv[0].Failed != commit(2002) {
		t.Fatalf("reverts = %+v", rv)
	}
	if !slices.Equal(r.copy.pushed, []string{commit(2001), commit(2002), commit(1001)}) || len(r.copy.commits) != 2 {
		t.Fatalf("pushed %v, commits %d", r.copy.pushed, len(r.copy.commits))
	}
	st := r.stack("web")
	if st.Deployed != commit(1001) || st.FailedCommit != commit(2002) || st.Paused != nil || st.LastRun.RolledBackTo != commit(1001) ||
		r.env() != envWith("1.2.0") {
		t.Fatalf("web = %+v / %+v, env %q", st, st.LastRun, r.env())
	}
}

func TestUpdatePushRejectedOnceCommitsAgain(t *testing.T) {
	r := updateRig(t)
	r.copy.pushErrs = []error{gitrepo.ErrPushRejected}
	res := r.update(step("1.0.0", "1.1.0"))
	if res[0].Result != ResultSucceeded || res[0].Commit != commit(2002) {
		t.Fatalf("results = %+v", res)
	}
	if len(r.copy.commits) != 2 || !slices.Equal(r.copy.pushed, []string{commit(2002)}) || r.copy.called("fetch") < 2 {
		t.Fatalf("commits %d, pushed %v, calls %v", len(r.copy.commits), r.copy.pushed, r.copy.calls)
	}
	if !strings.Contains(r.runLog(r.stack("web")), "rejected: the branch moved") {
		t.Fatal("the rejection is not logged")
	}
}

// wantRestarted checks a step that failed before its push: nothing on the branch, the stack started again at
// its deployed state.
func wantRestarted(t *testing.T, r *rig, res []StepResult, cause string) {
	t.Helper()
	if len(res) != 1 || res[0].Result != ResultRolledBack || res[0].Commit != commit(1) || !strings.Contains(res[0].Error, cause) {
		t.Fatalf("results = %+v", res)
	}
	st := r.stack("web")
	if st.Deployed != commit(1) || st.FailedCommit != "" || st.Paused != nil || st.LastRun.RolledBackTo != commit(1) {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
	if len(r.copy.pushed) != 0 || len(r.copy.reverts) != 0 || r.copy.called("ff") != 0 || r.env() != demoEnv {
		t.Fatalf("pushed %v, reverts %v, calls %v, env %q", r.copy.pushed, r.copy.reverts, r.copy.calls, r.env())
	}
	if !strings.Contains(r.runLog(st), "not needed: nothing was pushed") {
		t.Fatalf("log:\n%s", r.runLog(st))
	}
}

func TestUpdatePushRejectedTwiceStartsThePreviousStateAgain(t *testing.T) {
	r := updateRig(t)
	r.copy.pushErrs = []error{gitrepo.ErrPushRejected, gitrepo.ErrPushRejected}
	res := r.update(step("1.0.0", "1.1.0"))
	wantSteps(t, r, "stop", "backup", "stop", "up", "check")
	wantRestarted(t, r, res, "rejected")
}

func TestUpdateCommitAndPushErrorsPushNothing(t *testing.T) {
	for name, set := range map[string]func(*fakeCopy){
		"commit":   func(f *fakeCopy) { f.commitErrs = []error{errBoom} },
		"push":     func(f *fakeCopy) { f.pushErrs = []error{errBoom} },
		"diverged": func(f *fakeCopy) { f.diverged, f.remote = true, commit(5) },
		"branch moved": func(f *fakeCopy) {
			f.remote = commit(5)
			f.folders[[2]string{commit(1), commit(5)}] = []string{"web"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := updateRig(t)
			r.copy.set(set)
			res := r.update(step("1.0.0", "1.1.0"))
			wantRestarted(t, r, res, "commit")
		})
	}
}

func TestUpdateBackupFailureStartsThePreviousStateAgain(t *testing.T) {
	r := updateRig(t)
	r.dep.fail("backup", errBoom)
	res := r.update(step("1.0.0", "1.1.0"))
	wantSteps(t, r, "stop", "backup", "stop", "up", "check")
	wantRestarted(t, r, res, "backup")
	if r.copy.called("commit") != 0 {
		t.Fatal("committed after a failed backup")
	}
}

func TestUpdateRefusals(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		prepare func(r *rig)
		steps   []VersionStep
		want    error
	}{
		"local changes": {func(r *rig) {
			r.copy.set(func(f *fakeCopy) { f.changes = []gitrepo.Change{{Code: " M", Path: "web/.env"}} })
		}, nil, ErrLocalChanges},
		"changes elsewhere": {func(r *rig) {
			r.copy.set(func(f *fakeCopy) { f.changes = []gitrepo.Change{{Code: "??", Path: "notes.txt"}} })
		}, nil, ErrCopyChanges},
		"paused": {func(r *rig) {
			_, err := r.eng.store.Update(func(s State) (State, error) {
				cur := s.Stacks["web"]
				cur.Paused = &Pause{Time: r.clock.Now(), Run: "20260102T030405Z-0000abcd"}
				s.Stacks["web"] = cur
				return s, nil
			})
			if err != nil {
				r.t.Fatal(err)
			}
		}, nil, ErrPaused},
		"never deployed": {func(r *rig) {
			writeStack(r.t, r.wc, "fresh")
		}, nil, ErrNotDeployed},
		"undeployed changes": {func(r *rig) {
			r.copy.set(func(f *fakeCopy) {
				f.head = commit(3)
				f.folders[[2]string{commit(1), commit(3)}] = []string{"web"}
			})
		}, nil, ErrUndeployed},
		"stale plan":    {func(*rig) {}, []VersionStep{step("0.9.0", "1.1.0")}, ErrStalePlan},
		"broken chain":  {func(*rig) {}, []VersionStep{step("1.0.0", "1.1.0"), step("1.2.0", "2.0.0")}, ErrStalePlan},
		"unknown var":   {func(*rig) {}, []VersionStep{{Variable: "NOPE", From: "1.0.0", To: "1.1.0"}}, ErrStalePlan},
		"unknown stack": {func(r *rig) { _ = os.RemoveAll(filepath.Join(r.wc, "web")) }, nil, ErrUnknownStack},
	} {
		t.Run(name, func(t *testing.T) {
			r := updateRig(t)
			tc.prepare(r)
			stack := "web"
			if name == "never deployed" {
				stack = "fresh"
			}
			steps := tc.steps
			if steps == nil {
				steps = []VersionStep{step("1.0.0", "1.1.0")}
				if stack == "fresh" {
					steps = nil
				}
			}
			if _, err := r.eng.CheckUpdate(ctx, stack); !errors.Is(err, tc.want) && tc.steps == nil {
				t.Fatalf("check: %v, want %v", err, tc.want)
			}
			res, err := r.eng.Update(ctx, stack, steps)
			if !errors.Is(err, tc.want) || res != nil {
				t.Fatalf("update: %v %+v, want %v", err, res, tc.want)
			}
			if len(r.dep.stepsOf("web")) != 0 || r.copy.called("commit") != 0 {
				t.Fatal("something ran")
			}
			if snap, _ := r.eng.Snapshot(); len(snap.Active) != 0 {
				t.Fatalf("slot kept: %v", snap.Active)
			}
		})
	}
}

func TestUpdateRefusedWhileARunIsActive(t *testing.T) {
	r := updateRig(t)
	r.dep.block, r.dep.entered = make(chan struct{}), make(chan struct{}, 1)
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	<-r.dep.entered
	_, cerr := r.eng.CheckUpdate(context.Background(), "web")
	_, uerr := r.eng.Update(context.Background(), "web", []VersionStep{step("1.0.0", "1.1.0")})
	close(r.dep.block)
	r.eng.Wait()
	if !errors.Is(cerr, ErrRunActive) || !errors.Is(uerr, ErrRunActive) {
		t.Fatalf("check %v, update %v", cerr, uerr)
	}
}
