package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// Refusals of RollbackUpdate.
var (
	// ErrNoObservation means the stack has no update under observation, or another one than the caller named.
	ErrNoObservation = errors.New("the stack has no update under observation")
	// ErrBackupMissing means the backup the update made is gone; nothing was changed.
	ErrBackupMissing = errors.New("the backup of the update is missing")
)

// BackupChecker is implemented by a Deployer that can tell whether a backup it made still exists. The
// rollback of an update under observation asks it before it changes anything; without it a backup counts as
// missing.
type BackupChecker interface {
	HasBackup(ctx context.Context, stack stacks.Stack, id string) (bool, error)
}

// undo is the update a run rolls back and the container failure that made it do so.
type undo struct {
	Observation
	failure Failure
}

// undoable refuses the rollback of the update run/commit unless it is the stack's update under observation and
// the stack may be repaired.
func undoable(cur StackState, run, commit string) error {
	o := cur.Observe
	switch {
	case !repairable(cur):
		return ErrNotRepairable
	case o == nil || o.Run != run || o.Commit != commit || o.Commit != cur.Deployed:
		return ErrNoObservation
	}
	return nil
}

// RollbackUpdate rolls back the update under observation of the stack, whose containers failed with f in the
// observation time, as a run of its own with trigger observe: stop, restore exactly the update's backup,
// revert exactly the update's commit and follow it, up and check, as the rollback of a failed run does. When
// the stack changed on the branch since the update, the revert refuses and the rollback fails. A failed
// rollback stops and pauses the stack. commit is the update the caller saw. It returns once the run is
// started, under the stack's slot.
//
// A missing backup is refused with ErrBackupMissing before anything changes; a stack without backup rules is
// rolled back without its data. ErrNoObservation when commit is not the update under observation,
// ErrNotRepairable when the stack may not be repaired; otherwise it is refused as Trigger refuses it.
func (e *Engine) RollbackUpdate(ctx context.Context, name, commit string, f Failure) (RunInfo, error) {
	if e.cfg.Deployer == nil {
		return RunInfo{}, ErrNoDeployer
	}
	b, err := e.cfg.Source()
	if err != nil {
		return RunInfo{}, err
	}
	if err := e.lockCommand(ctx); err != nil {
		return RunInfo{}, err
	}
	defer e.lock.Unlock()
	st, _, err := e.runnable(ctx, b, name)
	if err != nil {
		return RunInfo{}, err
	}
	cur, _, err := e.Stack(name)
	if err != nil {
		return RunInfo{}, err
	}
	if cur.Observe == nil {
		return RunInfo{}, ErrNoObservation
	}
	o := *cur.Observe
	if err := undoable(cur, o.Run, commit); err != nil {
		return RunInfo{}, err
	}
	if err := e.backupPresent(ctx, st, o.Backup); err != nil {
		return RunInfo{}, err
	}
	return e.start(b, st, o.Commit, TriggerObserve, &undo{Observation: o, failure: f})
}

// backupPresent returns ErrBackupMissing unless the backup id exists; an empty id needs none.
func (e *Engine) backupPresent(ctx context.Context, st stacks.Stack, id string) error {
	if id == "" {
		return nil
	}
	c, ok := e.cfg.Deployer.(BackupChecker)
	if !ok {
		return fmt.Errorf("%w: %s cannot be checked", ErrBackupMissing, id)
	}
	has, err := c.HasBackup(ctx, st, id)
	switch {
	case err != nil:
		return fmt.Errorf("check the backup %s: %w", id, err)
	case !has:
		return fmt.Errorf("%w: %s", ErrBackupMissing, id)
	}
	return nil
}

// reverse is the run of a rollback of an update under observation.
func (r *run) reverse(ctx context.Context) outcome {
	u, log := r.undo, r.log
	what := u.failure.Kind
	if u.failure.Container != "" {
		what += " " + u.failure.Container
	}
	out := outcome{cause: fmt.Errorf("the stack failed within the observation time of update run %s (%s)", u.Run, r.e.text(what))}
	log.Printf("%v", out.cause)
	skip := ""
	if u.Backup == "" {
		skip = "the update made no backup: the stack has no backup rules"
	}
	at, err := r.rollback(ctx, fmt.Sprintf("the stack failed within the observation time of update run %s", u.Run), u.Backup, skip)
	if err != nil {
		out.result, out.rollbackErr = ResultRollbackFailed, err
		log.Printf("rollback failed: %v", err)
		r.stopAfterFailure()
		return out
	}
	out.result, out.at = ResultRolledBack, at
	return out
}
