package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// The actions of StackAction.
const (
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
)

// ErrStackStopped means a restart or a run was refused because the operator stopped the stack; only a start clears the
// mark.
var ErrStackStopped = errors.New("the stack was stopped by the operator")

// StackAction starts, stops or restarts the containers of a stack under its run slot, so it never overlaps a
// run. A stop sets the Stopped mark after the containers are stopped; a start clears it as soon as Compose has
// started the containers and then runs the check of the stack; a restart of a stopped stack is refused with
// ErrStackStopped. ErrNoDeployer without a deployer, ErrRunActive while a run is active. The containers are
// never created or removed: a start of a stack without containers fails.
func (e *Engine) StackAction(ctx context.Context, name, action string) error {
	d := e.cfg.Deployer
	if d == nil {
		return ErrNoDeployer
	}
	if action != ActionStart && action != ActionStop && action != ActionRestart {
		return fmt.Errorf("unknown stack action %q", action)
	}
	return e.WithStack(ctx, name, "stack "+action, func(ctx context.Context, st stacks.Stack, _ string) error {
		// The actions leave no run log, so the deployer gets none to write to.
		var log *Log
		switch action {
		case ActionStop:
			if err := d.Stop(ctx, st, log); err != nil {
				return err
			}
			_, err := e.setHold(ctx, name, true, func(s *StackState) **Hold { return &s.Stopped })
			return err
		case ActionStart:
			if err := d.Start(ctx, st, log); err != nil {
				return err
			}
			if _, err := e.setHold(ctx, name, false, func(s *StackState) **Hold { return &s.Stopped }); err != nil {
				return err
			}
		default:
			cur, _, err := e.Stack(name)
			if err != nil {
				return err
			}
			if cur.Stopped != nil {
				return ErrStackStopped
			}
			if err := d.Restart(ctx, st, log); err != nil {
				return err
			}
		}
		return d.Check(ctx, st, log)
	})
}
