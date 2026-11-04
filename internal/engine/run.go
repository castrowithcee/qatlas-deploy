package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// stopTimeout bounds the stop that leaves a stack stopped after a run that could not be rolled back.
const stopTimeout = 5 * time.Minute

// Event is the outcome of one finished run, for notifications.
type Event struct {
	// Kind is the run's result: ResultSucceeded, ResultFailed, ResultRolledBack or ResultRollbackFailed.
	Kind   string
	Stack  string
	Run    string
	Commit string
	// Error is the redacted reason of a run that did not succeed.
	Error string
}

// run is one run of a stack, from its start to its recorded result.
type run struct {
	e *Engine
	b Binding
	// st is the stack the steps are given; a rollback replaces it with the stack at the revert commit.
	st       stacks.Stack
	deployed string
	rec      RunRecord
	log      *Log
	// held says the run holds the working copy where it is; it gives the hold up only to move it itself.
	held bool
	// change is set for a step of an update; pushed says its commit is on the branch.
	change *VersionStep
	pushed bool
}

// outcome is what a run ended with. cause is why the run failed, rollbackErr why its rollback failed and
// at the commit the stack runs at after a success or a rollback.
type outcome struct {
	result      string
	cause       error
	rollbackErr error
	at          string
	backup      string
}

func (r *run) execute() {
	e := r.e
	defer e.wg.Done()
	out := r.perform()
	if r.held {
		e.releaseHold()
		r.held = false
	}
	rec := r.record(out)
	e.mu.Lock()
	delete(e.active, rec.Stack)
	e.mu.Unlock()
	e.notify(Event{Kind: rec.Result, Stack: rec.Stack, Run: rec.ID, Commit: rec.Commit, Error: rec.Error})
}

// perform runs the secured run under the run's own time limit.
func (r *run) perform() outcome {
	ctx, cancel := context.WithTimeout(r.e.base, r.e.cfg.RunTimeout)
	defer cancel()
	head := "run " + r.e.text(r.st.Name) + " at " + r.rec.Commit + " (" + r.rec.Trigger
	if c := r.change; c != nil {
		head += " " + r.e.text(c.Variable+" "+c.From+" -> "+c.To)
	}
	r.log.Step(head + ")")
	return r.secured(ctx)
}

// record closes the run log, stores the result of the run in the state and returns the record.
func (r *run) record(out outcome) RunRecord {
	e := r.e
	rec := r.rec
	rec.Duration = e.cfg.Clock.Now().Sub(rec.Started)
	rec.Result, rec.Backup = out.result, out.backup
	switch out.result {
	case ResultFailed, ResultRolledBack:
		rec.Error = e.text(out.cause.Error())
	case ResultRollbackFailed:
		rec.Error = e.text(out.cause.Error() + "; rollback failed: " + out.rollbackErr.Error())
	}
	if out.result == ResultRolledBack {
		rec.RolledBackTo = out.at
	}
	r.log.line("result", rec.Result+" "+rec.Error)
	r.log.close()
	// An update step that failed before its commit was pushed left nothing on the branch to mark as failed.
	marks := r.change == nil || r.pushed

	now := e.cfg.Clock.Now().UTC()
	var dropped []RunRecord
	_, uerr := e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		cur := s.Stacks[rec.Stack]
		x := rec
		cur.LastRun = &x
		switch rec.Result {
		case ResultSucceeded:
			cur.Deployed, cur.FailedCommit = out.at, ""
		case ResultFailed:
			if marks {
				cur.FailedCommit = rec.Commit
			}
		case ResultRolledBack:
			// The stack folder at the revert commit equals the old deployed state, so the next poll does not
			// start the failed version again.
			cur.Deployed = out.at
			if marks {
				cur.FailedCommit = rec.Commit
			}
		case ResultRollbackFailed:
			if marks {
				cur.FailedCommit = rec.Commit
			}
			cur.Paused = &Pause{Time: now, Run: rec.ID, Reason: e.text(out.rollbackErr.Error())}
			cur.Findings = append([]Finding{{FindingPaused, e.pauseDetail(cur.Paused)}}, cur.Findings...)
		}
		if cur.Pending != "" && (cur.Pending == rec.Commit || cur.Pending == out.at) {
			cur.Pending = ""
		}
		s.Stacks[rec.Stack] = cur
		dropped = s.addRun(rec)
		return s, nil
	})
	if uerr != nil {
		e.cfg.Logger.Error("run result not recorded", "run", rec.ID, "error", uerr)
	}
	ids := make([]string, len(dropped))
	for i, d := range dropped {
		ids[i] = d.ID
	}
	e.logs.remove(ids...)
	return rec
}

func (e *Engine) notify(ev Event) {
	if e.cfg.Events == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			e.cfg.Logger.Error("the event hook panicked", "run", ev.Run, "panic", fmt.Sprint(p))
		}
	}()
	e.cfg.Events(ev)
}

// step logs the step and runs fn; a panic of the deployer is the step's error.
func (r *run) step(name string, fn func() error) (err error) {
	r.log.Step(name)
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the deployer panicked: %v", p)
		}
	}()
	return fn()
}

// secured is the run itself: stop, backup, pull, up, check, and the rollback when a step after the stop fails.
// An update step commits and pushes its version change after the backup; see advance.
func (r *run) secured(ctx context.Context) outcome {
	d, st, log := r.e.cfg.Deployer, r.st, r.log
	if err := r.step("stop", func() error { return d.Stop(ctx, st, log) }); err != nil {
		return outcome{result: ResultFailed, cause: fmt.Errorf("stop: %w", err)}
	}

	var (
		out      outcome
		backupID string
		failedAt string
		noData   string // why the rollback restores no data
		started  bool
	)
	err := r.step("backup", func() error {
		if r.deployed == "" {
			noData = "the stack was never deployed"
			log.Printf("none: %s", noData)
			return nil
		}
		id, err := d.Backup(ctx, st, r.deployed, log)
		switch {
		case errors.Is(err, ErrNoBackup):
			noData = "the stack has no backup rules"
			log.Printf("none: %s", noData)
			return nil
		case err != nil:
			noData = "the backup failed"
			return err
		}
		backupID, out.backup = id, r.e.text(id)
		log.Printf("backup %s", id)
		return nil
	})
	if err != nil {
		failedAt = "backup"
	}
	if err == nil && r.change != nil {
		if err = r.advance(ctx); err != nil {
			failedAt = "commit"
		}
		st = r.st
	}
	if err == nil {
		if err = r.step("pull", func() error { return d.Pull(ctx, st, log) }); err != nil {
			failedAt = "pull"
		}
	}
	if err == nil {
		started = true
		if err = r.step("up", func() error { return d.Up(ctx, st, log) }); err != nil {
			failedAt = "up"
		}
	}
	if err == nil {
		if err = r.step("check", func() error { return d.Check(ctx, st, log) }); err != nil {
			failedAt = "check"
		}
	}
	if err == nil {
		out.result, out.at = ResultSucceeded, r.rec.Commit
		return out
	}
	out.cause = fmt.Errorf("%s: %w", failedAt, err)

	if r.deployed == "" {
		// Nothing to go back to: the new stack is stopped and the commit is marked failed.
		log.Step("rollback")
		log.Printf("not possible: the stack was never deployed")
		r.stopAfterFailure()
		out.result = ResultFailed
		return out
	}
	if r.e.base.Err() != nil {
		out.result, out.rollbackErr = ResultRollbackFailed, errors.New("the service stopped during the run; no rollback was attempted")
		log.Printf("%v", out.rollbackErr)
		return out
	}
	// The rollback has a time limit of its own: the run's may be what the failed step used up.
	rctx, cancel := context.WithTimeout(r.e.base, r.e.cfg.RunTimeout)
	defer cancel()
	skip := noData
	if skip == "" && !started {
		skip = "the new version was never started, so the data is unchanged"
	}
	at, rerr := r.rollback(rctx, failedAt, backupID, skip)
	if rerr != nil {
		out.result, out.rollbackErr = ResultRollbackFailed, rerr
		log.Printf("rollback failed: %v", rerr)
		r.stopAfterFailure()
		return out
	}
	out.result, out.at = ResultRolledBack, at
	return out
}

// rollback brings the stack back to its deployed state: stop, restore the run's backup unless skip says why
// not, revert the stack folder on the branch and follow it, start and check. An update step whose commit was
// not pushed has nothing to revert: the working copy and the stack folder never left the deployed state. It
// returns the commit the stack runs at.
func (r *run) rollback(ctx context.Context, failedAt, backup, skip string) (string, error) {
	d, log := r.e.cfg.Deployer, r.log
	log.Step("rollback")
	if err := r.step("stop", func() error { return d.Stop(ctx, r.st, log) }); err != nil {
		return "", fmt.Errorf("stop: %w", err)
	}
	err := r.step("restore", func() error {
		if skip != "" {
			log.Printf("skipped: %s", skip)
			return nil
		}
		log.Printf("backup %s", backup)
		return d.Restore(ctx, r.st, backup, log)
	})
	if err != nil {
		return "", fmt.Errorf("restore backup %s: %w", backup, err)
	}
	at := r.deployed
	if r.change == nil || r.pushed {
		if at, err = r.revert(ctx, failedAt); err != nil {
			return "", err
		}
	} else {
		log.Step("revert")
		log.Printf("not needed: nothing was pushed and the working copy stayed at %s", at)
	}
	if err := r.step("up", func() error { return d.Up(ctx, r.st, log) }); err != nil {
		return "", fmt.Errorf("up of the previous state: %w", err)
	}
	if err := r.step("check", func() error { return d.Check(ctx, r.st, log) }); err != nil {
		return "", fmt.Errorf("check of the previous state: %w", err)
	}
	return at, nil
}

// revert moves the working copy to a commit whose stack folder equals the deployed state: a revert commit on
// the freshly fetched branch, pushed as a fast-forward, or the branch itself when it already has that state.
// It waits until no other run relies on the working copy and keeps everyone else off it meanwhile; it returns
// holding the run's hold again.
func (r *run) revert(ctx context.Context, failedAt string) (string, error) {
	e, log, wc := r.e, r.log, r.b.Copy
	log.Step("revert")
	e.releaseHold()
	r.held = false
	if err := e.exclusive(ctx); err != nil {
		return "", fmt.Errorf("wait for the other runs: %w", err)
	}
	defer func() {
		e.share()
		r.held = true
	}()

	if err := wc.Fetch(ctx); err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	changes, err := wc.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("status: %w", err)
	}
	// Checked before anything is pushed: a working copy that cannot follow the revert must not get one.
	if local, dirty := classify(changes); dirty {
		var paths []string
		for _, p := range local {
			paths = append(paths, p...)
		}
		slices.Sort(paths)
		detail := "changes outside the stacks"
		if len(paths) > 0 {
			detail = e.localDetail(paths)
		}
		return "", fmt.Errorf("the working copy has local changes and cannot follow a revert (%s); nothing was pushed", detail)
	}
	reason := fmt.Sprintf("run %s failed at the %s step", r.rec.ID, failedAt)
	var at string
	for attempt := 1; at == ""; attempt++ {
		c, err := wc.RevertStack(ctx, r.st.Folder, r.deployed, r.rec.Commit, reason)
		if errors.Is(err, gitrepo.ErrNothingToRevert) {
			log.Printf("nothing to revert: the branch already has the deployed state of the stack")
			if at, err = wc.RemoteHead(ctx); err != nil {
				return "", err
			}
			break
		}
		if err != nil {
			return "", fmt.Errorf("revert: %w", err)
		}
		log.Printf("revert commit %s", c)
		err = r.step("push", func() error { return wc.Push(ctx, c) })
		switch {
		case err == nil:
			at = c
		case errors.Is(err, gitrepo.ErrPushRejected) && attempt == 1:
			log.Printf("rejected: the branch moved; fetching and reverting again")
			if err := wc.Fetch(ctx); err != nil {
				return "", fmt.Errorf("fetch: %w", err)
			}
		default:
			return "", fmt.Errorf("push: %w", err)
		}
	}
	if err := r.follow(ctx, at, "revert"); err != nil {
		return "", err
	}
	return at, nil
}

// follow fast-forwards the working copy to at, the pushed commit, and gives the run the stack as it is there.
// The caller has the working copy to itself.
func (r *run) follow(ctx context.Context, at, what string) error {
	e, log, wc := r.e, r.log, r.b.Copy
	if err := r.step("fast-forward", func() error { return wc.FastForward(ctx) }); err != nil {
		return err
	}
	head, err := wc.Head(ctx)
	if err != nil {
		return err
	}
	if head != at {
		return fmt.Errorf("the working copy is at %s after the fast-forward, not at %s", e.text(head), at)
	}
	list, err := stacks.Load(r.b.Dir)
	if err != nil {
		return fmt.Errorf("read stacks: %w", err)
	}
	i := slices.IndexFunc(list, func(s stacks.Stack) bool { return s.Name == r.st.Name })
	switch {
	case i < 0:
		return fmt.Errorf("the stack is missing at the %s commit", what)
	case list[i].Err != nil:
		return fmt.Errorf("the stack cannot be read at the %s commit: %s", what, e.text(list[i].Err.Error()))
	}
	r.st = list[i]
	log.Printf("the working copy is at %s", at)
	return nil
}

// stopAfterFailure leaves the stack stopped once it can be neither deployed nor rolled back.
func (r *run) stopAfterFailure() {
	ctx, cancel := context.WithTimeout(r.e.base, stopTimeout)
	defer cancel()
	d := r.e.cfg.Deployer
	if err := r.step("stop", func() error { return d.Stop(ctx, r.st, r.log) }); err != nil {
		r.log.Printf("the stack could not be stopped: %v", err)
	}
}

// The working copy's position is shared by every active run and claim (a hold, see start and claim) and
// moved only by poll's fast-forward, which needs no run active, and by a rollback, which needs exclusive. New
// holds are taken only with the working-copy lock held, so exclusive owning that lock while holds is zero
// keeps everyone off the position. Nobody waits for holds with the lock held, which keeps two rollbacks and
// a poll from waiting on each other.

// exclusive waits until no hold is left and returns with the working-copy lock held. The caller holds
// neither a hold nor the lock.
func (e *Engine) exclusive(ctx context.Context) error {
	for {
		e.mu.Lock()
		n, idle := e.holds, e.idle
		e.mu.Unlock()
		if n > 0 {
			select {
			case <-idle:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		if err := e.lockContext(ctx); err != nil {
			return err
		}
		e.mu.Lock()
		n = e.holds
		e.mu.Unlock()
		if n == 0 {
			return nil
		}
		e.lock.Unlock()
	}
}

// share turns the exclusive access back into a hold, without a gap another mover could use.
func (e *Engine) share() {
	e.mu.Lock()
	e.holds++
	e.mu.Unlock()
	e.lock.Unlock()
}

func (e *Engine) releaseHold() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.holds--
	if e.holds == 0 {
		close(e.idle)
		e.idle = make(chan struct{})
	}
}

// lockContext takes the working-copy lock unless ctx ends first; a lock taken too late is released.
func (e *Engine) lockContext(ctx context.Context) error {
	got := make(chan struct{})
	go func() {
		e.lock.Lock()
		close(got)
	}()
	select {
	case <-got:
		return nil
	case <-ctx.Done():
		go func() {
			<-got
			e.lock.Unlock()
		}()
		return ctx.Err()
	}
}

// Resume clears the pause of a stack and returns what it was. It starts no run: the failed commit stays
// marked, so the stack runs again on a new commit or a manual trigger. ErrNotPaused when it is not paused.
func (e *Engine) Resume(name string) (Pause, error) {
	var was Pause
	_, err := e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		cur, ok := s.Stacks[name]
		if !ok || cur.Paused == nil {
			return s, ErrNotPaused
		}
		was = *cur.Paused
		cur.Paused = nil
		cur.Findings = slices.DeleteFunc(cur.Findings, func(f Finding) bool { return f.Kind == FindingPaused })
		s.Stacks[name] = cur
		return s, nil
	})
	return was, err
}
