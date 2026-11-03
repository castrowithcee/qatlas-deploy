package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

var errBoom = errors.New("boom from the demo stack")

// rollbackRig deploys the stacks at commit 1 and makes commit 2, which changes all of them, the fetched one.
// The test sets the failures and polls.
func rollbackRig(t *testing.T, names ...string) *rig {
	t.Helper()
	r := newRig(t, names...)
	r.deployAll()
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = names
		f.calls = nil
	})
	return r
}

// withEvents restarts the engine of the rig with an event hook and returns what it saw.
func (r *rig) withEvents() func() []Event {
	var mu sync.Mutex
	var seen []Event
	r.cfg.Events = func(ev Event) {
		mu.Lock()
		seen = append(seen, ev)
		mu.Unlock()
	}
	r.eng = r.start()
	return func() []Event {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

func (r *rig) runLog(st StackState) string {
	r.t.Helper()
	text, err := r.eng.Log(st.LastRun.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	return text
}

const backupID = "backup-of-00000000" // the fake's backup of commit 1

func wantSteps(t *testing.T, r *rig, want ...string) {
	t.Helper()
	if got := r.dep.stepsOf("web"); !slices.Equal(got, want) {
		t.Fatalf("steps = %q\nwant    %q", got, want)
	}
}

// wantRolledBack checks the end state of a successful rollback to the revert commit at.
func wantRolledBack(t *testing.T, r *rig, at string) StackState {
	t.Helper()
	st := r.stack("web")
	if st.LastRun == nil || st.LastRun.Result != ResultRolledBack || st.Deployed != at || st.FailedCommit != commit(2) ||
		st.Paused != nil || st.LastRun.RolledBackTo != at || !strings.Contains(st.LastRun.Error, "boom") {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
	if head, _ := r.copy.Head(context.Background()); head != at {
		t.Fatalf("head = %s, want %s", head, at)
	}
	// the rolled-back state is current: the next poll starts nothing
	r.dep.mu.Lock()
	r.dep.calls = nil
	r.dep.mu.Unlock()
	if res := r.poll(); len(res.Started) != 0 || hasFinding(r.stack("web"), FindingFailedCommit) {
		t.Fatalf("poll after the rollback: %+v %+v", res, r.stack("web"))
	}
	return st
}

// wantPaused checks the end state of a failed rollback.
func wantPaused(t *testing.T, r *rig, reason string) StackState {
	t.Helper()
	st := r.stack("web")
	if st.LastRun == nil || st.LastRun.Result != ResultRollbackFailed || st.Deployed != commit(1) ||
		st.FailedCommit != commit(2) || st.Paused == nil || st.Paused.Run != st.LastRun.ID ||
		!strings.Contains(st.Paused.Reason, reason) || !hasFinding(st, FindingPaused) {
		t.Fatalf("web = %+v / %+v / %+v", st, st.LastRun, st.Paused)
	}
	if steps := r.dep.stepsOf("web"); steps[len(steps)-1] != "stop" {
		t.Fatalf("the stack is not stopped at the end: %v", steps)
	}
	return st
}

func TestCheckFailureRollsBackWithData(t *testing.T) {
	r := rollbackRig(t, "web")
	events := r.withEvents()
	r.dep.fail("check", errBoom)
	r.poll()
	wantSteps(t, r, "stop", "backup", "pull", "up", "check", "stop", "restore "+backupID, "up", "check")
	st := wantRolledBack(t, r, commit(1001))
	if st.LastRun.Backup != backupID {
		t.Fatalf("backup = %q", st.LastRun.Backup)
	}
	if rv := r.copy.reverts; len(rv) != 1 || rv[0].Stack != "web" || rv[0].Deployed != commit(1) || rv[0].Failed != commit(2) ||
		rv[0].Base != commit(2) || !strings.Contains(rv[0].Reason, st.LastRun.ID) || !strings.Contains(rv[0].Reason, "check") {
		t.Fatalf("reverts = %+v", rv)
	}
	if !slices.Equal(r.copy.pushed, []string{commit(1001)}) {
		t.Fatalf("pushed = %v", r.copy.pushed)
	}
	text := r.runLog(st)
	for _, want := range []string{"step backup", "step rollback", "step restore", "step revert", "step push", "step fast-forward",
		"revert commit " + commit(1001), "result rolled-back"} {
		if !strings.Contains(text, want) {
			t.Fatalf("log lacks %q:\n%s", want, text)
		}
	}
	if ev := events(); len(ev) != 1 || ev[0] != (Event{Kind: ResultRolledBack, Stack: "web", Run: st.LastRun.ID, Commit: commit(2), Error: st.LastRun.Error}) {
		t.Fatalf("events = %+v", ev)
	}
}

func TestUpFailureRestoresTheData(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("up", errBoom)
	r.poll()
	wantSteps(t, r, "stop", "backup", "pull", "up", "stop", "restore "+backupID, "up", "check")
	wantRolledBack(t, r, commit(1001))
}

func TestPullFailureSkipsTheRestore(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("pull", errBoom)
	r.poll()
	wantSteps(t, r, "stop", "backup", "pull", "stop", "up", "check")
	st := wantRolledBack(t, r, commit(1001))
	if !strings.Contains(r.runLog(st), "skipped: the new version was never started") {
		t.Fatalf("log:\n%s", r.runLog(st))
	}
}

func TestBackupFailureRollsBackWithoutRestore(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("backup", errBoom)
	r.poll()
	wantSteps(t, r, "stop", "backup", "stop", "up", "check")
	st := wantRolledBack(t, r, commit(1001))
	if st.LastRun.Backup != "" || !strings.Contains(r.runLog(st), "skipped: the backup failed") {
		t.Fatalf("run = %+v\n%s", st.LastRun, r.runLog(st))
	}
}

func TestStackWithoutBackupRulesRollsBackWithoutRestore(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.noRules = true
	r.dep.fail("check", errBoom)
	r.poll()
	wantSteps(t, r, "stop", "backup", "pull", "up", "check", "stop", "up", "check")
	st := wantRolledBack(t, r, commit(1001))
	if text := r.runLog(st); !strings.Contains(text, "none: the stack has no backup rules") || !strings.Contains(text, "skipped: the stack has no backup rules") {
		t.Fatalf("log:\n%s", text)
	}
}

func TestSucceededRunReportsAnEvent(t *testing.T) {
	r := rollbackRig(t, "web")
	events := r.withEvents()
	r.poll()
	wantSteps(t, r, "stop", "backup", "pull", "up", "check")
	st := r.stack("web")
	if st.Deployed != commit(2) || st.LastRun.Result != ResultSucceeded || st.LastRun.Backup != backupID {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
	if ev := events(); len(ev) != 1 || ev[0].Kind != ResultSucceeded || ev[0].Commit != commit(2) || ev[0].Error != "" {
		t.Fatalf("events = %+v", ev)
	}
}

func TestStopFailureFailsTheRunWithoutRollback(t *testing.T) {
	r := rollbackRig(t, "web")
	events := r.withEvents()
	r.dep.fail("stop", errBoom)
	r.poll()
	wantSteps(t, r, "stop")
	st := r.stack("web")
	if st.LastRun.Result != ResultFailed || st.Deployed != commit(1) || st.FailedCommit != commit(2) || st.Paused != nil {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
	if r.copy.called("revert") != 0 {
		t.Fatal("reverted")
	}
	if ev := events(); len(ev) != 1 || ev[0].Kind != ResultFailed {
		t.Fatalf("events = %+v", ev)
	}
}

func TestRestoreFailurePausesTheStack(t *testing.T) {
	r := rollbackRig(t, "web")
	events := r.withEvents()
	r.dep.fail("check", errBoom)
	r.dep.fail("restore "+backupID, errors.New("archive unreadable"))
	r.poll()
	wantSteps(t, r, "stop", "backup", "pull", "up", "check", "stop", "restore "+backupID, "stop")
	st := wantPaused(t, r, "archive unreadable")
	if r.copy.called("revert")+r.copy.called("push") != 0 {
		t.Fatal("reverted after a failed restore")
	}
	if ev := events(); len(ev) != 1 || ev[0].Kind != ResultRollbackFailed || !strings.Contains(ev[0].Error, "archive unreadable") ||
		!strings.Contains(ev[0].Error, "boom") || ev[0].Run != st.LastRun.ID {
		t.Fatalf("events = %+v", ev)
	}
}

func TestRevertConflictPausesTheStack(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	r.copy.revertErrs = []error{gitrepo.ErrRevertConflict}
	r.poll()
	wantSteps(t, r, "stop", "backup", "pull", "up", "check", "stop", "restore "+backupID, "stop")
	wantPaused(t, r, "changed after the failed commit")
	if r.copy.called("push") != 0 || r.copy.called("ff") != 1 { // the poll's fast-forward only
		t.Fatal("pushed or moved after a conflict")
	}
}

func TestPushRejectedOnceRevertsAgainOnTheNewBranch(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	r.copy.pushErrs = []error{gitrepo.ErrPushRejected}
	fetches := 0
	r.copy.onFetch = func(f *fakeCopy) {
		fetches++
		if fetches == 3 { // poll, rollback, retry: someone else pushed meanwhile
			f.remote = commit(3)
		}
	}
	r.poll()
	if rv := r.copy.reverts; len(rv) != 2 || rv[1].Base != commit(3) {
		t.Fatalf("reverts = %+v", rv)
	}
	if !slices.Equal(r.copy.pushed, []string{commit(1002)}) {
		t.Fatalf("pushed = %v", r.copy.pushed)
	}
	wantRolledBack(t, r, commit(1002))
}

func TestPushRejectedTwicePausesTheStack(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	r.copy.pushErrs = []error{gitrepo.ErrPushRejected, gitrepo.ErrPushRejected}
	r.poll()
	wantPaused(t, r, "push rejected")
	if r.copy.called("push") != 2 || len(r.copy.pushed) != 0 || r.copy.called("ff") != 1 {
		t.Fatalf("calls = %v", r.copy.calls)
	}
}

func TestPushErrorPausesTheStack(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	r.copy.pushErrs = []error{errors.New("permission denied by the demo remote")}
	r.poll()
	wantPaused(t, r, "permission denied")
	if r.copy.called("push") != 1 || r.copy.called("revert") != 1 {
		t.Fatalf("calls = %v", r.copy.calls)
	}
}

func TestRevertConflictAfterRejectedPushPausesTheStack(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	r.copy.pushErrs = []error{gitrepo.ErrPushRejected}
	r.copy.revertErrs = []error{nil, gitrepo.ErrRevertConflict}
	r.poll()
	wantPaused(t, r, "changed after the failed commit")
}

func TestLocalChangesBlockTheRevert(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	// the change appears while the run is active, after the poll fast-forwarded
	r.dep.hook = func(_ context.Context, step string, _ stacks.Stack) error {
		if step == "check" {
			r.copy.set(func(f *fakeCopy) { f.changes = []gitrepo.Change{{Code: " M", Path: "web/compose.yaml"}} })
		}
		return nil
	}
	r.poll()
	wantPaused(t, r, "local changes")
	if r.copy.called("revert")+r.copy.called("push") != 0 {
		t.Fatalf("calls = %v", r.copy.calls)
	}
}

func TestFailedFastForwardPausesTheStack(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	r.dep.hook = func(_ context.Context, step string, _ stacks.Stack) error {
		if step == "check" { // after the poll's own fast-forward
			r.copy.set(func(f *fakeCopy) { f.ffErr = errors.New("fast-forward failed: not possible") })
		}
		return nil
	}
	r.poll()
	wantSteps(t, r, "stop", "backup", "pull", "up", "check", "stop", "restore "+backupID, "stop")
	wantPaused(t, r, "fast-forward failed")
}

func TestCheckAfterRollbackFailurePausesTheStack(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom, errors.New("still unhealthy"))
	r.poll()
	wantSteps(t, r, "stop", "backup", "pull", "up", "check", "stop", "restore "+backupID, "up", "check", "stop")
	wantPaused(t, r, "still unhealthy")
	// the revert is published and followed; the stack still counts as deployed at its old state
	if head, _ := r.copy.Head(context.Background()); head != commit(1001) {
		t.Fatalf("head = %s", head)
	}
}

func TestNothingToRevertFollowsTheBranch(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	r.copy.revertErrs = []error{gitrepo.ErrNothingToRevert}
	r.copy.onFetch = func(f *fakeCopy) {
		if f.head == commit(2) {
			f.remote = commit(4) // someone reverted the stack on the branch already
		}
	}
	r.poll()
	if r.copy.called("push") != 0 {
		t.Fatal("pushed without a revert commit")
	}
	wantSteps(t, r, "stop", "backup", "pull", "up", "check", "stop", "restore "+backupID, "up", "check")
	r.copy.onFetch = nil
	wantRolledBack(t, r, commit(4))
}

func TestPausedStackGetsNoRunsUntilResumed(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	r.copy.revertErrs = []error{gitrepo.ErrRevertConflict}
	r.poll()
	pause := wantPaused(t, r, "changed after").Paused

	// a new commit for the stack starts nothing and keeps the finding
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(3)
		f.folders[[2]string{commit(1), commit(3)}] = []string{"web"}
	})
	r.dep.mu.Lock()
	r.dep.calls = nil
	r.dep.mu.Unlock()
	if res := r.poll(); len(res.Started) != 0 || len(r.dep.got()) != 0 {
		t.Fatalf("paused stack ran: %+v", res)
	}
	st := r.stack("web")
	if !hasFinding(st, FindingPaused) || st.Pending != commit(3) || st.Findings[0].Kind != FindingPaused {
		t.Fatalf("web = %+v", st)
	}
	if _, err := r.eng.Trigger(context.Background(), "web"); !errors.Is(err, ErrPaused) {
		t.Fatalf("trigger err = %v", err)
	}
	// a human may still back up or restore
	if err := r.eng.WithStack(context.Background(), "web", "backup", func(context.Context, stacks.Stack, string) error { return nil }); err != nil {
		t.Fatalf("WithStack: %v", err)
	}

	got, err := r.eng.Resume("web")
	if err != nil || got != *pause {
		t.Fatalf("resume = %+v, %v", got, err)
	}
	if _, err := r.eng.Resume("web"); !errors.Is(err, ErrNotPaused) {
		t.Fatalf("second resume err = %v", err)
	}
	if _, err := r.eng.Resume("nope"); !errors.Is(err, ErrNotPaused) {
		t.Fatalf("resume unknown err = %v", err)
	}
	st = r.stack("web")
	if st.Paused != nil || hasFinding(st, FindingPaused) || st.FailedCommit != commit(2) {
		t.Fatalf("after resume = %+v", st)
	}
	if len(r.dep.got()) != 0 {
		t.Fatal("resume started a run")
	}
	// the next poll runs the new commit
	r.dep.mu.Lock()
	r.dep.errs = nil
	r.dep.mu.Unlock()
	if res := r.poll(); !slices.Equal(res.Started, []string{"web"}) || r.stack("web").Deployed != commit(3) {
		t.Fatalf("after resume: %+v %+v", res, r.stack("web"))
	}
}

func TestResumeWithoutState(t *testing.T) {
	r := newRig(t, "web")
	if _, err := r.eng.Resume("web"); !errors.Is(err, ErrNotPaused) {
		t.Fatalf("err = %v", err)
	}
}

func TestPauseSurvivesRestart(t *testing.T) {
	r := rollbackRig(t, "web")
	r.dep.fail("check", errBoom)
	r.copy.revertErrs = []error{gitrepo.ErrRevertConflict}
	r.poll()
	r.eng.Close()
	r.eng = r.start()
	if st := r.stack("web"); st.Paused == nil {
		t.Fatalf("web = %+v", st)
	}
	if _, err := r.eng.Trigger(context.Background(), "web"); !errors.Is(err, ErrPaused) {
		t.Fatalf("trigger err = %v", err)
	}
}

func TestServiceStopDuringRunPausesWithoutRollback(t *testing.T) {
	r := rollbackRig(t, "web")
	entered := make(chan struct{})
	r.dep.hook = func(ctx context.Context, step string, _ stacks.Stack) error {
		if step == "up" {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	if _, err := r.eng.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	r.eng.Close()
	st := r.stack("web")
	if st.LastRun.Result != ResultRollbackFailed || st.Paused == nil || !strings.Contains(st.Paused.Reason, "service stopped") {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
	if r.copy.called("revert") != 0 {
		t.Fatal("reverted during shutdown")
	}
}

// TestRollbackWaitsForOtherRuns: a rollback moves the working copy only once no other run relies on it, and
// a poll meanwhile neither moves it nor deadlocks.
func TestRollbackWaitsForOtherRuns(t *testing.T) {
	r := rollbackRig(t, "web", "db")
	r.dep.trace = r.copy.rec
	dbUp, releaseDB := make(chan struct{}), make(chan struct{})
	webChecked := make(chan struct{})
	r.dep.hook = func(_ context.Context, step string, st stacks.Stack) error {
		switch {
		case st.Name == "db" && step == "up":
			close(dbUp)
			<-releaseDB
		case st.Name == "web" && step == "check":
			select {
			case <-webChecked:
				return nil // the check of the previous state
			default:
				<-dbUp
				close(webChecked)
				return errBoom
			}
		}
		return nil
	}
	if _, err := r.eng.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "web waits for the revert", func() bool { return slices.Contains(r.dep.stepsOf("web"), "restore "+backupID) })
	time.Sleep(20 * time.Millisecond)
	if n := r.copy.called("revert"); n != 0 {
		t.Fatal("reverted while another run was active")
	}
	// a poll while the rollback waits does not move the working copy
	if _, err := r.eng.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.copy.called("ff") != 1 { // the first poll's
		t.Fatalf("ff = %d", r.copy.called("ff"))
	}
	close(releaseDB)
	r.eng.Wait()

	calls := r.copy.calls
	dbDone := slices.Index(calls, "db:check")
	revert := slices.Index(calls, "revert")
	if dbDone < 0 || revert < dbDone {
		t.Fatalf("order = %v", calls)
	}
	if st := r.stack("db"); st.Deployed != commit(2) || st.LastRun.Result != ResultSucceeded {
		t.Fatalf("db = %+v", st)
	}
	if st := r.stack("web"); st.Deployed != commit(1001) || st.LastRun.Result != ResultRolledBack {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
}

// TestTwoRollbacksAtOnce: both stacks fail together; each rollback moves the working copy alone and starts
// its previous state before the other moves it.
func TestTwoRollbacksAtOnce(t *testing.T) {
	r := rollbackRig(t, "web", "db")
	r.dep.trace = r.copy.rec
	var once sync.Map
	var arrived sync.WaitGroup
	arrived.Add(2)
	r.dep.hook = func(_ context.Context, step string, st stacks.Stack) error {
		if step != "check" {
			return nil
		}
		if _, again := once.LoadOrStore(st.Name, true); again {
			return nil
		}
		arrived.Done()
		arrived.Wait() // both runs are active before either fails
		return errBoom
	}
	r.poll()

	for _, name := range []string{"web", "db"} {
		if st := r.stack(name); st.LastRun.Result != ResultRolledBack {
			t.Fatalf("%s = %+v / %+v", name, st, st.LastRun)
		}
	}
	// a rollback's fetch, revert, push and fast-forward and the start of its previous state follow each other
	// without a step of the other stack or another git call in between
	calls := r.copy.calls
	k := 0
	for i, c := range calls {
		if c != "revert" {
			continue
		}
		owner := r.copy.reverts[k].Stack
		k++
		want := []string{"fetch", "revert", "push", "ff", owner + ":up", owner + ":check"}
		if i == 0 || i+5 > len(calls) || !slices.Equal(calls[i-1:i+5], want) {
			t.Fatalf("calls = %v", calls)
		}
	}
	// each revert is built on the branch the previous one pushed
	if rv := r.copy.reverts; len(rv) != 2 || rv[0].Base != commit(2) || rv[1].Base != commit(1001) {
		t.Fatalf("reverts = %+v", rv)
	}
	a, b := r.stack(r.copy.reverts[0].Stack), r.stack(r.copy.reverts[1].Stack)
	if a.Deployed != commit(1001) || b.Deployed != commit(1002) {
		t.Fatalf("deployed = %s, %s", a.Deployed, b.Deployed)
	}
}

func TestRollbackWaitIsBounded(t *testing.T) {
	r := rollbackRig(t, "web", "db")
	r.copy.set(func(f *fakeCopy) { f.folders[[2]string{commit(1), commit(2)}] = []string{"web"} })
	r.cfg.RunTimeout = 200 * time.Millisecond
	r.eng = r.start()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	done := make(chan error, 1)
	r.dep.hook = func(_ context.Context, step string, st stacks.Stack) error {
		if st.Name != "web" || step != "check" || slices.Contains(r.dep.stepsOf("web"), "restore "+backupID) {
			return nil
		}
		// a backup of db that does not end holds the working copy while web rolls back
		held := make(chan struct{})
		go func() {
			done <- r.eng.WithStack(context.Background(), "db", "backup", func(context.Context, stacks.Stack, string) error {
				close(held)
				<-release
				return nil
			})
		}()
		select {
		case <-held:
		case err := <-done:
			t.Errorf("WithStack: %v", err)
		}
		return errBoom
	}
	if _, err := r.eng.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the run ends", func() bool {
		st, _, _ := r.eng.Stack("web")
		return st.LastRun != nil && st.LastRun.Commit == commit(2)
	})
	wantPaused(t, r, "wait for the other runs")
	if r.copy.called("revert") != 0 {
		t.Fatal("reverted while a backup held the working copy")
	}
}

func TestPanickingEventHookDoesNotBreakTheRun(t *testing.T) {
	r := rollbackRig(t, "web")
	r.cfg.Events = func(Event) { panic("hook exploded") }
	r.eng = r.start()
	r.poll()
	if st := r.stack("web"); st.Deployed != commit(2) {
		t.Fatalf("web = %+v", st)
	}
	if snap, _ := r.eng.Snapshot(); len(snap.Active) != 0 {
		t.Fatalf("active = %v", snap.Active)
	}
}
