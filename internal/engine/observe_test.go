package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
)

var appExited = Failure{Kind: FailureExited, Container: "web-app-1", Since: time.Date(2026, 1, 2, 3, 20, 0, 0, time.UTC)}

func (r *rig) resetSteps() {
	r.dep.mu.Lock()
	r.dep.calls, r.dep.steps = nil, nil
	r.dep.mu.Unlock()
}

// rollbackUpdate rolls back the update under observation and waits for the run.
func (r *rig) rollbackUpdate(commit string) (RunInfo, error) {
	r.t.Helper()
	info, err := r.eng.RollbackUpdate(context.Background(), "web", commit, appExited)
	r.eng.Wait()
	return info, err
}

func TestUpdateStartsTheObservationOfItsCommit(t *testing.T) {
	r := updateRig(t)
	res := r.update(step("1.0.0", "1.1.0"))
	st := r.stack("web")
	o := st.Observe
	now := r.clock.Now()
	if o == nil || o.Run != res[0].Run || o.From != commit(1) || o.Commit != commit(2001) || o.Backup != backupID ||
		!o.Since.Equal(now) || !o.Until.Equal(now.Add(time.Hour)) {
		t.Fatalf("observation = %+v", o)
	}
	snap, err := r.eng.Snapshot()
	if err != nil || snap.State.Stacks["web"].Observe == nil || snap.State.Stacks["web"].Observe.Commit != commit(2001) {
		t.Fatalf("snapshot %+v, %v", snap.State.Stacks["web"].Observe, err)
	}
	// it survives a restart of the service
	r.eng.Close()
	r.eng = r.start()
	if again := r.stack("web").Observe; again == nil || again.Run != o.Run || again.Commit != o.Commit || !again.Until.Equal(o.Until) {
		t.Fatalf("after a restart: %+v", again)
	}
}

func TestObserveZeroStartsNoObservation(t *testing.T) {
	r := updateRig(t)
	writeSettings(t, r.wc, "web", "version: 1\nupdate:\n  observe: 0\n")
	r.update(step("1.0.0", "1.1.0"))
	if st := r.stack("web"); st.LastRun.Result != ResultSucceeded || st.Observe != nil {
		t.Fatalf("web = %+v / %+v", st.Observe, st.LastRun)
	}
	if _, err := r.rollbackUpdate(commit(2001)); !errors.Is(err, ErrNoObservation) {
		t.Fatalf("rollback without observation: %v", err)
	}
}

func TestObservationTimeComesFromTheSettings(t *testing.T) {
	r := updateRig(t)
	writeSettings(t, r.wc, "web", "version: 1\nupdate:\n  observe: 15\n")
	r.update(step("1.0.0", "1.1.0"))
	if o := r.stack("web").Observe; o == nil || o.Until.Sub(o.Since) != 15*time.Minute {
		t.Fatalf("observation = %+v", o)
	}
}

func TestAnyOtherRunEndsTheObservation(t *testing.T) {
	r := updateRig(t)
	r.update(step("1.0.0", "1.1.0"))
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	r.eng.Wait()
	if st := r.stack("web"); st.LastRun.Trigger != TriggerManual || st.LastRun.Result != ResultSucceeded || st.Observe != nil {
		t.Fatalf("after a manual run: %+v / %+v", st.Observe, st.LastRun)
	}
}

func TestStepwiseUpdateObservesOnlyItsLastStep(t *testing.T) {
	r := updateRig(t)
	res := r.update(step("1.0.0", "1.2.0"), step("1.2.0", "2.3.0"), step("2.3.0", "3.1.0"))
	o := r.stack("web").Observe
	if o == nil || o.Run != res[2].Run || o.From != commit(2002) || o.Commit != commit(2003) {
		t.Fatalf("observation = %+v", o)
	}
	r.resetSteps()
	if _, err := r.rollbackUpdate(commit(2003)); err != nil {
		t.Fatal(err)
	}
	if rv := r.copy.reverts; len(rv) != 1 || rv[0].Deployed != commit(2002) || rv[0].Failed != commit(2003) {
		t.Fatalf("reverts = %+v", rv)
	}
	if st := r.stack("web"); st.LastRun.Result != ResultRolledBack || r.env() != envWith("2.3.0") {
		t.Fatalf("web = %+v, env %q", st.LastRun, r.env())
	}
}

func TestRollbackUpdateRevertsExactlyTheUpdateWithItsBackup(t *testing.T) {
	r := updateRig(t)
	events := r.withEvents()
	r.update(step("1.0.0", "1.1.0"))
	r.resetSteps()
	r.clock.Advance(20 * time.Minute)
	info, err := r.rollbackUpdate(commit(2001))
	if err != nil || info.Commit != commit(2001) || info.ID == "" {
		t.Fatalf("rollback %+v, %v", info, err)
	}
	wantSteps(t, r, "has "+backupID, "stop", "restore "+backupID, "up", "check")
	rv := r.copy.reverts
	if len(rv) != 1 || rv[0].Stack != "web" || rv[0].Deployed != commit(1) || rv[0].Failed != commit(2001) ||
		!strings.Contains(rv[0].Reason, "observation time") {
		t.Fatalf("reverts = %+v", rv)
	}
	st := r.stack("web")
	if st.LastRun.ID != info.ID || st.LastRun.Trigger != TriggerObserve || st.LastRun.Result != ResultRolledBack ||
		st.LastRun.Commit != commit(2001) || st.Deployed != commit(1001) || st.LastRun.RolledBackTo != commit(1001) ||
		st.FailedCommit != commit(2001) || st.Observe != nil || st.Paused != nil || r.env() != demoEnv ||
		!strings.Contains(st.LastRun.Error, "observation time") || !strings.Contains(st.LastRun.Error, "exited web-app-1") {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
	if runs, _ := r.eng.Runs("web"); len(runs) != 3 || runs[0].ID != info.ID || runs[0].Trigger != TriggerObserve {
		t.Fatalf("runs = %+v", runs)
	}
	text := r.runLog(st)
	for _, want := range []string{"(observe)", "step rollback", "backup " + backupID, "step revert", "result rolled-back"} {
		if !strings.Contains(text, want) {
			t.Fatalf("log lacks %q:\n%s", want, text)
		}
	}
	ev := events()
	if last := ev[len(ev)-1]; last.Kind != ResultRolledBack || last.Cause != CauseObservation || last.Run != info.ID ||
		last.Commit != commit(2001) {
		t.Fatalf("events = %+v", ev)
	}
	if ev[0].Cause != "" {
		t.Fatalf("the update's event has a cause: %+v", ev[0])
	}
	// a second rollback finds nothing to roll back
	if _, err := r.rollbackUpdate(commit(2001)); !errors.Is(err, ErrNoObservation) {
		t.Fatalf("second rollback: %v", err)
	}
}

func TestRollbackUpdateWithAMissingBackupChangesNothing(t *testing.T) {
	r := updateRig(t)
	r.update(step("1.0.0", "1.1.0"))
	before := r.stack("web")
	r.resetSteps()
	r.dep.gone = []string{backupID}
	_, err := r.rollbackUpdate(commit(2001))
	if !errors.Is(err, ErrBackupMissing) || !strings.Contains(err.Error(), backupID) {
		t.Fatalf("err = %v", err)
	}
	wantSteps(t, r, "has "+backupID)
	st := r.stack("web")
	if len(r.copy.reverts) != 0 || st.Deployed != before.Deployed || st.LastRun.ID != before.LastRun.ID || st.Observe == nil {
		t.Fatalf("changed: reverts %+v, web %+v", r.copy.reverts, st)
	}
	if snap, _ := r.eng.Snapshot(); len(snap.Active) != 0 {
		t.Fatalf("slot kept: %v", snap.Active)
	}
}

func TestRollbackUpdateWithoutBackupRulesRestoresNoData(t *testing.T) {
	r := updateRig(t)
	r.dep.noRules = true
	r.update(step("1.0.0", "1.1.0"))
	if o := r.stack("web").Observe; o == nil || o.Backup != "" {
		t.Fatalf("observation = %+v", o)
	}
	r.resetSteps()
	if _, err := r.rollbackUpdate(commit(2001)); err != nil {
		t.Fatal(err)
	}
	wantSteps(t, r, "stop", "up", "check")
	st := r.stack("web")
	if st.LastRun.Result != ResultRolledBack || !strings.Contains(r.runLog(st), "skipped: the update made no backup") {
		t.Fatalf("web = %+v", st.LastRun)
	}
}

func TestFailedRollbackUpdatePausesTheStack(t *testing.T) {
	for name, fail := range map[string]func(r *rig){
		"check": func(r *rig) { r.dep.fail("check", errBoom) },
		"branch moved": func(r *rig) {
			r.copy.set(func(f *fakeCopy) { f.revertErrs = []error{gitrepo.ErrRevertConflict} })
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := updateRig(t)
			events := r.withEvents()
			r.update(step("1.0.0", "1.1.0"))
			fail(r)
			info, err := r.rollbackUpdate(commit(2001))
			if err != nil {
				t.Fatal(err)
			}
			st := r.stack("web")
			if st.LastRun.Result != ResultRollbackFailed || st.Paused == nil || st.Paused.Run != info.ID || !hasFinding(st, FindingPaused) ||
				st.Observe != nil {
				t.Fatalf("web = %+v / %+v", st, st.LastRun)
			}
			if steps := r.dep.stepsOf("web"); steps[len(steps)-1] != "stop" {
				t.Fatalf("the stack was not left stopped: %q", steps)
			}
			ev := events()
			if last := ev[len(ev)-1]; last.Kind != ResultRollbackFailed || last.Cause != CauseObservation {
				t.Fatalf("events = %+v", ev)
			}
		})
	}
}

func TestRollbackUpdateRefusals(t *testing.T) {
	r := updateRig(t)
	if _, err := r.rollbackUpdate(commit(1)); !errors.Is(err, ErrNoObservation) {
		t.Fatalf("without an update: %v", err)
	}
	r.update(step("1.0.0", "1.1.0"))
	if _, err := r.rollbackUpdate(commit(1)); !errors.Is(err, ErrNoObservation) {
		t.Fatalf("another commit: %v", err)
	}
	if _, err := r.eng.SetPause(context.Background(), "web", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.rollbackUpdate(commit(2001)); !errors.Is(err, ErrNotRepairable) {
		t.Fatalf("operator pause: %v", err)
	}
	if _, err := r.eng.SetPause(context.Background(), "web", false); err != nil {
		t.Fatal(err)
	}
	release, err := r.eng.claim("web", "backup")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.RollbackUpdate(context.Background(), "web", commit(2001), appExited); !errors.Is(err, ErrRunActive) {
		t.Fatalf("busy slot: %v", err)
	}
	release()
	if len(r.copy.reverts) != 0 || r.stack("web").Observe == nil {
		t.Fatalf("a refusal changed something")
	}
}

func TestStateWithoutObservationLoads(t *testing.T) {
	r := newRig(t, "web")
	dir := filepath.Join(r.state, engineDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"schema":1,"stacks":{"web":{"deployed":"` + commit(1) + `"}}}`
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := r.stack("web"); st.Deployed != commit(1) || st.Observe != nil {
		t.Fatalf("web = %+v", st)
	}
	bad := StackState{Deployed: commit(1), Observe: &Observation{Run: "x", From: commit(1), Commit: commit(2)}}
	if err := validateState(State{Schema: stateSchema, Stacks: map[string]StackState{"web": bad}}); err == nil {
		t.Fatal("an invalid observation was accepted")
	}
}
